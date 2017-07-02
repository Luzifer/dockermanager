package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Luzifer/dockermanager/config"
	"github.com/Luzifer/go_helpers/str"
	log "github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
)

const (
	labelIsManaged   = "io.luzifer.dockermanager.managed"
	labelConfigHash  = "io.luzifer.dockermanager.cfghash"
	labelIsScheduled = "io.luzifer.dockermanager.scheduler"

	strTrue = "true"
)

var (
	lastStartContainerCall = time.Now()

	pullLock     = map[string]bool{}
	pullLockLock sync.RWMutex
)

func getImages(dangling bool) []docker.APIImages {
	images, err := dockerClient.ListImages(docker.ListImagesOptions{
		All: false,
	})

	if err != nil {
		log.Errorf("Unable to list images: %s", err)
		return nil
	}

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
		log.Debugf("Removing dangling image: %s", v.ID)
		dockerClient.RemoveImage(v.ID)
	}
}

func removeNotRequiredImages() {
	if !params.ManageFullHost {
		// If we're not responsible for the full host don't remove images
		return
	}

	required := []string{}
	for _, v := range *cfg {
		if str.StringInSlice(hostname, v.Hosts) || str.StringInSlice("ALL", v.Hosts) {
			required = append(required, fmt.Sprintf("%s:%s", v.Image, v.Tag))
		}
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
			log.Debugf("Removing not required image: %s", i.ID)
			dockerClient.RemoveImage(i.ID)
		}
	}
}

func refreshImages() {
	for _, v := range *cfg {
		if str.StringInSlice(hostname, v.Hosts) || str.StringInSlice("ALL", v.Hosts) {
			pullImage(v.Image, v.Tag)
		}
	}
}

func pullImage(image, tag string) {
	pullLockLock.Lock()
	if pullLock[image+":"+tag] {
		log.Debugf("Image %q is already pulling, starting no new pull", image+":"+tag)
		pullLockLock.Unlock()
		return
	}
	pullLock[image+":"+tag] = true
	pullLockLock.Unlock()

	defer func() {
		pullLockLock.Lock()
		pullLock[image+":"+tag] = false
		pullLockLock.Unlock()
	}()

	auth := docker.AuthConfiguration{}

	reginfo := strings.SplitN(image, "/", 2)
	if len(reginfo) == 2 {
		for s, a := range authConfig.Configs {
			if strings.Contains(s, reginfo[0]) {
				auth = a
			}
		}
	}

	log.Debugf("Refreshing repo %s:%s...", image, tag)
	if err := dockerClient.PullImage(docker.PullImageOptions{
		Repository: image,
		Tag:        tag,
	}, auth); err != nil {
		log.WithFields(log.Fields{
			"repo": image + ":" + tag,
		}).Errorf("An error occurred while image pulling: %s", err)
	}
}

func bootContainer(name string, cfg config.ContainerConfig) {
	var (
		container *docker.Container
		err       error
	)

	cs, err := cfg.Checksum()
	if err != nil {
		log.Errorf("Unable to calculate checksum for container %q: %s", name, err)
		return
	}

	labels := map[string]string{}
	if labels != nil {
		for k, v := range cfg.Labels {
			labels[k] = v
		}
	}
	labels[labelConfigHash] = cs
	labels[labelIsManaged] = strTrue

	if cfg.StartTimes != "" {
		labels[labelIsScheduled] = strTrue
	}

	volumes, binds := parseMounts(cfg.Volumes)

	newcfg := &docker.Config{
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Image:        strings.Join([]string{cfg.Image, cfg.Tag}, ":"),
		Env:          cfg.Environment,
		Cmd:          cfg.Command,
		Labels:       labels,
		Volumes:      volumes,
	}

	hostConfig := &docker.HostConfig{
		Binds:        binds,
		Links:        cfg.Links,
		Privileged:   false,
		PortBindings: make(map[docker.Port][]docker.PortBinding),
		CapAdd:       cfg.AddCapabilities,
	}

	for _, v := range cfg.Ports {
		s := strings.Split(v.Local, ":")
		hostConfig.PortBindings[docker.Port(v.Container)] = []docker.PortBinding{{
			HostIP:   s[0],
			HostPort: s[1],
		}}
	}

	log.Debugf("Creating container %s", name)
	container, err = dockerClient.CreateContainer(docker.CreateContainerOptions{
		Name:       name,
		Config:     newcfg,
		HostConfig: hostConfig,
	})

	if err != nil {
		switch {
		case strings.Contains(err.Error(), "is already in use by container"):
			go cleanContainers()
		case strings.Contains(err.Error(), "already exists"):
			go cleanContainers()
		}
		log.Errorf("Unable to create container '%s': %s", name, err)
		return
	}

	log.Debugf("Starting container %s", container.Name)
	dockerClient.StartContainer(container.Name, nil)
}

func listContainers(listDeadContainers bool) ([]docker.APIContainers, error) {
	return dockerClient.ListContainers(docker.ListContainersOptions{
		All: listDeadContainers,
	})
}

func getExpectedRunningNames() []string {
	expectedRunning := []string{}
	for name, containerCfg := range *cfg {
		if containerCfg.ShouldBeRunning(hostname, lastStartContainerCall) {
			expectedRunning = append(expectedRunning, name)
		}
	}
	return expectedRunning
}

func stopUnexpectedContainers() {
	expectedRunning := getExpectedRunningNames()

	// Stop containers not expected to be running
	currentRunning, err := listContainers(false)
	if err != nil {
		log.Errorf("Unable to list running containers: %s", err)
		return
	}
	for _, v := range currentRunning {
		allowed := false

		containerDetails, err := dockerClient.InspectContainer(v.ID)
		if err != nil {
			log.Errorf("Unable to inspect container %q: %s", v.ID, err)
			continue
		}

		_, isManaged := containerDetails.Config.Labels[labelIsManaged]
		if !params.ManageFullHost && !isManaged {
			allowed = true
		}

		if containerDetails.Config.Labels[labelIsScheduled] == strTrue {
			// If it's a scheduled container keep it running
			allowed = true
		}

		for _, n := range v.Names {
			if str.StringInSlice(strings.Trim(n, "/"), expectedRunning) {
				allowed = true
			}
		}
		if !allowed {
			log.Debugf("Stopping container %s as it is not expected to be running.", v.ID)
			if err := dockerClient.StopContainer(v.ID, 5); err != nil {
				log.Errorf("Unable to stop container %q", v.ID)
				continue
			}
		}
	}
}

func removeDeprecatedContainers() {
	expectedRunning := getExpectedRunningNames()

	// Stop containers who are of deprecated images
	currentRunning, err := listContainers(false)
	if err != nil {
		log.Errorf("Unable to list running containers: %s", err)
		return
	}
	images := getImages(false)
	for _, n := range expectedRunning {
		repoName := strings.Join([]string{(*cfg)[n].Image, (*cfg)[n].Tag}, ":")
		currentImageID := "0"
		for _, i := range images {
			if str.StringInSlice(repoName, i.RepoTags) {
				currentImageID = i.ID
			}
		}
		if currentImageID == "0" {
			log.Debugf("Found no image ID for repo %q, starting background pull", strings.Join([]string{(*cfg)[n].Image, (*cfg)[n].Tag}, ":"))
			go pullImage((*cfg)[n].Image, (*cfg)[n].Tag)
			continue
		}
		for _, v := range currentRunning {
			containerDetails, err := dockerClient.InspectContainer(v.ID)
			if err != nil {
				log.Errorf("Unable to inspect container %q: %s", v.ID, err)
				continue
			}

			cs, err := (*cfg)[n].Checksum()
			if err != nil {
				log.Errorf("Unable to calculate checksum for container %q: %s", v.ID, err)
				continue
			}

			if str.StringInSlice(fmt.Sprintf("/%s", n), v.Names) {
				needsUpdate := false
				if !strings.HasPrefix(currentImageID, containerDetails.Image) {
					log.Infof("Container %s has a new image version.", n)
					needsUpdate = true
				}
				if containerDetails.Config.Labels[labelConfigHash] != cs {
					log.Infof("Container %s has a configuration update.", n)
					needsUpdate = true
				}
				if needsUpdate && !(*cfg)[n].UpdateAllowedAt(time.Now()) {
					log.Infof("Image %s has update but container %s (%s) is not allowed to update now.", v.Image, n, v.ID)
					needsUpdate = false
				}

				if needsUpdate {
					stopWaitTime := (*cfg)[n].StopTimeout
					if stopWaitTime == 0 {
						stopWaitTime = 5
					}

					log.Debugf("Image: %s Current: %s", containerDetails.Image, currentImageID)
					log.Debugf("Stopping deprecated container %s", v.ID)
					if err = dockerClient.StopContainer(v.ID, stopWaitTime); err != nil {
						log.Errorf("Unable to stop container %q", v.ID)
						continue
					}

					log.Debugf("Removing deprecated container %s", v.ID)
					dockerClient.RemoveContainer(docker.RemoveContainerOptions{
						ID: v.ID,
					})

					if currentRunning, err = listContainers(false); err != nil {
						log.Errorf("Unable to list running containers: %s", err)
						return
					}
				}
			}
		}
	}
}

func startExpectedContainers() {
	expectedRunning := getExpectedRunningNames()
	lastStartContainerCall = time.Now()

	// Start expected containers
	currentRunning, err := listContainers(false)
	if err != nil {
		log.Errorf("Unable to list running containers: %s", err)
		return
	}
	runningNames := []string{}
	for _, v := range currentRunning {
		for _, n := range v.Names {
			runningNames = append(runningNames, strings.Trim(n, "/"))
		}
	}
	for _, n := range expectedRunning {
		if !str.StringInSlice(n, runningNames) {
			bootContainer(n, (*cfg)[n])
		}
	}
}

func cleanContainers() {
	runningContainers, err := listContainers(true)
	if err != nil {
		log.Errorf("Unable to list running containers: %s", err)
		return
	}

	for _, v := range runningContainers {
		_, isManaged := v.Labels[labelIsManaged]

		if strings.HasPrefix(v.Status, "Exited") || strings.HasPrefix(v.Status, "Dead") || (strings.HasPrefix(v.Status, "Created") && isManaged) {
			log.Debugf("Removing container %s (Status %s)", v.ID, v.Status)
			if err := dockerClient.RemoveContainer(docker.RemoveContainerOptions{
				ID:    v.ID,
				Force: true,
			}); err != nil {
				log.Errorf("Unable to remove container %s (Status %s): %s", v.ID, v.Status, err)
			}
		}
	}
}

func parseMounts(mountIn []string) (volumes map[string]struct{}, binds []string) {
	volumes = make(map[string]struct{})
	for _, m := range mountIn {
		if len(m) == 0 {
			continue
		}

		parts := strings.Split(m, ":")
		if len(parts) != 2 && len(parts) != 3 {
			log.Errorf("Invalid default mount: %s", m)
			continue
		}

		binds = append(binds, m)
		volumes[parts[1]] = struct{}{}
	}

	return
}
