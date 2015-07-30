package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/fsouza/go-dockerclient"
)

var (
	lastStartContainerCall = time.Now()
)

func getImages(dangling bool) []docker.APIImages {
	images, err := dockerClient.ListImages(docker.ListImagesOptions{
		All: false,
	})
	orFail(err)

	nowSeconds := time.Now().Unix()
	response := []docker.APIImages{}

	for _, v := range images {
		if dangling {
			if len(v.RepoTags) == 1 && v.RepoTags[0] == "<none>:<none>" && (nowSeconds-v.Created) > 3600 {
				response = append(response, v)
			}
		} else {
			if len(v.RepoTags) != 1 || v.RepoTags[0] != "<none>:<none>" {
				response = append(response, v)
			}
		}
	}

	return response
}

func cleanDangling() {
	images := getImages(true)
	for _, v := range images {
		log.Printf("Removing dangling image: %s", v.ID)
		dockerClient.RemoveImage(v.ID)
	}
}

func removeNotRequiredImages() {
	required := []string{}
	for _, v := range *cfg {
		required = append(required, fmt.Sprintf("%s:%s", v.Image, v.Tag))
	}

	currentImages := getImages(false)
	for _, i := range currentImages {
		found := false
		for _, t := range i.RepoTags {
			for _, r := range required {
				if r == t {
					found = true
				}
			}
		}

		if !found {
			log.Printf("Removing not required image: %s", i.ID)
			dockerClient.RemoveImage(i.ID)
		}
	}
}

func refreshImages() {
	for _, v := range *cfg {
		pullImage(v.Image, v.Tag)
	}
}

func pullImage(image, tag string) {
	auth := docker.AuthConfiguration{}

	reginfo := strings.SplitN(image, "/", 2)
	if len(reginfo) == 2 {
		for s, a := range authConfig.Configs {
			if strings.Contains(s, fmt.Sprintf("://%s/", reginfo[0])) {
				auth = a
			}
		}
	}

	log.Printf("Refreshing repo %s:%s...", image, tag)
	err := dockerClient.PullImage(docker.PullImageOptions{
		Repository: image,
		Tag:        tag,
	}, auth)
	orLog(err)
}

func bootContainer(name string, cfg containerConfig) {
	var (
		container *docker.Container
		err       error
		bo        = backoff.NewExponentialBackOff()
	)

	cs, err := cfg.checksum()
	orFail(err)
	if err != nil {
		return
	}

	labels := cfg.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	labels["io.luzifer.dockermanager.cfghash"] = cs
	labels["io.luzifer.dockermanager.managed"] = "true"

	if cfg.StartTimes != "" {
		labels["io.luzifer.dockermanager.scheduler"] = "true"
	}

	newcfg := &docker.Config{
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Image:        strings.Join([]string{cfg.Image, cfg.Tag}, ":"),
		Env:          cfg.Environment,
		Cmd:          cfg.Command,
		Labels:       labels,
	}

	bo.MaxElapsedTime = time.Minute
	err = backoff.Retry(func() error {
		log.Printf("Creating container %s", name)
		container, err = dockerClient.CreateContainer(docker.CreateContainerOptions{
			Name:   name,
			Config: newcfg,
		})
		orLog(err)
		if err != nil {
			if strings.Contains(err.Error(), "no such image") {
				pullImage(cfg.Image, cfg.Tag)
			}
			if strings.Contains(err.Error(), "is already in use by container") {
				cleanContainers()
			}
		}
		return err
	}, bo)

	if err != nil {
		log.Printf("Unable to start container '%s': %s", name, err)
		return
	}

	hostConfig := docker.HostConfig{
		Binds:        cfg.Volumes,
		Links:        cfg.Links,
		Privileged:   false,
		PortBindings: make(map[docker.Port][]docker.PortBinding),
	}

	for _, v := range cfg.Ports {
		s := strings.Split(v.Local, ":")
		hostConfig.PortBindings[docker.Port(v.Container)] = []docker.PortBinding{docker.PortBinding{
			HostIP:   s[0],
			HostPort: s[1],
		}}
	}

	log.Printf("Starting container %s", container.Name)
	dockerClient.StartContainer(container.Name, &hostConfig)
}

func listRunningContainers() ([]docker.APIContainers, error) {
	containers, err := dockerClient.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	orLog(err)
	return containers, err
}

func getExpectedRunningNames() []string {
	expectedRunning := []string{}
	for name, containerCfg := range *cfg {
		if containerCfg.shouldBeRunning(serfElector.MyName) {
			expectedRunning = append(expectedRunning, name)
		}
	}
	return expectedRunning
}

func stopUnexpectedContainers() {
	expectedRunning := getExpectedRunningNames()

	// Stop containers not expected to be running
	currentRunning, err := listRunningContainers()
	orLog(err)
	if err != nil {
		return
	}
	for _, v := range currentRunning {
		allowed := false

		containerDetails, err := dockerClient.InspectContainer(v.ID)
		orFail(err)

		_, isManaged := containerDetails.Config.Labels["io.luzifer.dockermanager.managed"]
		if !params.ManageFullHost && !isManaged {
			allowed = true
		}

		if containerDetails.Config.Labels["io.luzifer.dockermanager.scheduler"] == "true" {
			// If it's a scheduled container keep it running
			allowed = true
		}

		for _, n := range v.Names {
			if stringInSlice(strings.Trim(n, "/"), expectedRunning) {
				allowed = true
			}
		}
		if !allowed {
			log.Printf("Stopping container %s as it is not expected to be running.", v.ID)
			err := dockerClient.StopContainer(v.ID, 5)
			orFail(err)
			time.Sleep(time.Second * 5)
		}
	}
}

func removeDeprecatedContainers() {
	expectedRunning := getExpectedRunningNames()

	// Stop containers who are of deprecated images
	currentRunning, err := listRunningContainers()
	orLog(err)
	if err != nil {
		return
	}
	images := getImages(false)
	for _, n := range expectedRunning {
		repoName := strings.Join([]string{(*cfg)[n].Image, (*cfg)[n].Tag}, ":")
		currentImageID := "0"
		for _, i := range images {
			if stringInSlice(repoName, i.RepoTags) {
				currentImageID = i.ID
			}
		}
		if currentImageID == "0" {
			log.Printf("Found no image ID for repo %s", strings.Join([]string{(*cfg)[n].Image, (*cfg)[n].Tag}, ":"))
			continue
		}
		for _, v := range currentRunning {
			containerDetails, err := dockerClient.InspectContainer(v.ID)
			orFail(err)

			cs, err := (*cfg)[n].checksum()

			if stringInSlice(fmt.Sprintf("/%s", n), v.Names) {
				needsUpdate := false
				if !strings.HasPrefix(currentImageID, containerDetails.Image) {
					log.Printf("Container %s has a new image version.", n)
					needsUpdate = true
				}
				if containerDetails.Config.Labels["io.luzifer.dockermanager.cfghash"] != cs {
					log.Printf("Container %s has a configuration update.", n)
					needsUpdate = true
				}
				if needsUpdate && !timeAllowed((*cfg)[n].UpdateTimes) {
					log.Printf("Image %s has update but container %s (%s) is not allowed to update now.", v.Image, n, v.ID)
					needsUpdate = false
				}

				if needsUpdate {
					stopWaitTime := (*cfg)[n].StopTimeout
					if stopWaitTime == 0 {
						stopWaitTime = 5
					}

					log.Printf("Image: %s Current: %s", containerDetails.Image, currentImageID)
					log.Printf("Stopping deprecated container %s", v.ID)
					err := dockerClient.StopContainer(v.ID, stopWaitTime)
					orFail(err)
					time.Sleep(time.Second * time.Duration(stopWaitTime))

					log.Printf("Removing deprecated container %s", v.ID)
					dockerClient.RemoveContainer(docker.RemoveContainerOptions{
						ID: v.ID,
					})

					currentRunning, err = listRunningContainers()
					orFail(err)
				}
			}
		}
	}
}

func startExpectedContainers() {
	expectedRunning := getExpectedRunningNames()
	lastStartContainerCall = time.Now()

	// Start expected containers
	currentRunning, err := listRunningContainers()
	orLog(err)
	if err != nil {
		return
	}
	runningNames := []string{}
	for _, v := range currentRunning {
		for _, n := range v.Names {
			runningNames = append(runningNames, strings.Trim(n, "/"))
		}
	}
	for _, n := range expectedRunning {
		if !stringInSlice(n, runningNames) {
			bootContainer(n, (*cfg)[n])
		}
	}
}

func cleanContainers() {
	runningContainers, err := dockerClient.ListContainers(docker.ListContainersOptions{
		All: true,
	})
	orLog(err)
	if err != nil {
		return
	}

	for _, v := range runningContainers {
		if strings.HasPrefix(v.Status, "Exited") {
			log.Printf("Removing stopped container %s", v.ID)
			err := dockerClient.RemoveContainer(docker.RemoveContainerOptions{
				ID: v.ID,
			})
			orLog(err)
		}
	}
}
