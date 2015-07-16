package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/fsouza/go-dockerclient"
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
	newcfg := &docker.Config{
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Image:        strings.Join([]string{cfg.Image, cfg.Tag}, ":"),
		Env:          cfg.Environment,
		Cmd:          cfg.Command,
	}

	var (
		container *docker.Container
		err       error
		bo        = backoff.NewExponentialBackOff()
	)
	backoff.Retry(func() error {
		log.Printf("Creating container %s", name)
		container, err = dockerClient.CreateContainer(docker.CreateContainerOptions{
			Name:   name,
			Config: newcfg,
		})
		orLog(err)
		if err != nil && strings.Contains(err.Error(), "no such image") {
			pullImage(cfg.Image, cfg.Tag)
		}
		return err
	}, bo)

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
		if stringInSlice(serfElector.MyName, containerCfg.Hosts) || stringInSlice("ALL", containerCfg.Hosts) {
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

			if stringInSlice(fmt.Sprintf("/%s", n), v.Names) && !strings.HasPrefix(currentImageID, containerDetails.Image) {
				if timeAllowed((*cfg)[n].UpdateTimes) == false {
					log.Printf("Image %s has update but container %s is not allowed to update now.", v.Image, v.ID)
					continue
				}
				log.Printf("Image: %s Current: %s", containerDetails.Image, currentImageID)
				log.Printf("Stopping deprecated container %s", v.ID)
				err := dockerClient.StopContainer(v.ID, 5)
				orFail(err)
				time.Sleep(time.Second * 5)

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

func startExpectedContainers() {
	expectedRunning := getExpectedRunningNames()

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
