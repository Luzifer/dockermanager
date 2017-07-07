package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Luzifer/dockermanager/config"
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
	pullLock     = map[string]bool{}
	pullLockLock sync.RWMutex
)

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

func bootContainer(name string, ccfg *config.ContainerConfig) error {
	var (
		container *docker.Container
		err       error
	)

	cs, err := ccfg.Checksum()
	if err != nil {
		return fmt.Errorf("Unable to calculate checksum: %s", err)
	}

	labels := map[string]string{}
	if labels != nil {
		for k, v := range ccfg.Labels {
			labels[k] = v
		}
	}
	labels[labelConfigHash] = cs
	labels[labelIsManaged] = strTrue

	if ccfg.StartTimes != "" {
		labels[labelIsScheduled] = strTrue
	}

	volumes, binds := parseMounts(ccfg.Volumes)

	newcfg := &docker.Config{
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Image:        strings.Join([]string{ccfg.Image, ccfg.Tag}, ":"),
		Env:          ccfg.Environment,
		Cmd:          ccfg.Command,
		Labels:       labels,
		Volumes:      volumes,
	}

	hostConfig := &docker.HostConfig{
		Binds:        binds,
		Links:        ccfg.Links,
		Privileged:   false,
		PortBindings: make(map[docker.Port][]docker.PortBinding),
		CapAdd:       ccfg.AddCapabilities,
	}

	for _, v := range ccfg.Ports {
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
		return fmt.Errorf("Unable to create container: %s", err)
	}

	log.Infof("Starting container %q...", container.Name)
	if err := dockerClient.StartContainer(container.Name, nil); err != nil {
		return fmt.Errorf("Unable to start created container: %s", err)
	}

	return nil
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
