package main

import (
	"fmt"
	"strings"

	"github.com/Luzifer/dockermanager/config"
	"github.com/fsouza/go-dockerclient"
	log "github.com/sirupsen/logrus"
)

const (
	labelIsManaged   = "io.luzifer.dockermanager.managed"
	labelConfigHash  = "io.luzifer.dockermanager.cfghash"
	labelIsScheduled = "io.luzifer.dockermanager.scheduler"

	strTrue = "true"
)

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
