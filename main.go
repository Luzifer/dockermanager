package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/robfig/cron"
)

var serfElector *serfMasterElector
var actionTimer *time.Timer
var remoteActionTimer *time.Timer
var cleanupTimer *time.Timer
var configTimer *time.Timer
var dockerClient *docker.Client
var cfg *config
var authConfig *docker.AuthConfigurations

// #### CONFIG ####
func reloadConfig(params *dockerManagerParams) {
	log.Print("Loading config...")
	var err error

	var newCfg *config

	if _, err := os.Stat(params.Config); err == nil {
		newCfg, err = loadConfigFromFile(params.Config)
	} else {
		newCfg, err = loadConfigFromURL(params.Config)
	}
	if err == nil {
		cfg = newCfg
	}
}

// #### MAIN ####

func main() {
	params := GetStartupParameters()

	serfElector = newSerfMasterElector()
	go serfElector.Run(params.SerfAddress)

	// Create a timer but stop it immediately for later usage in remote actions
	remoteActionTimer = time.NewTimer(time.Second * 60)
	remoteActionTimer.Stop()

	var err error
	if params.DockerCertDir == "" {
		dockerClient, err = docker.NewClient(params.DockerHost)
	} else {
		dockerClient, err = docker.NewTLSClient(
			params.DockerHost,
			path.Join(params.DockerCertDir, "cert.pem"),
			path.Join(params.DockerCertDir, "key.pem"),
			path.Join(params.DockerCertDir, "ca.pem"),
		)
	}
	orFail(err)

	// Load local .dockercfg
	authConfig = &docker.AuthConfigurations{}
	auth, err := docker.NewAuthConfigurationsFromDockerCfg()
	if err == nil {
		authConfig = auth
	} else {
		log.Printf("Could not read authconfig: %s\n", err)
	}

	c := cron.New()

	// Refresh images
	c.AddFunc(fmt.Sprintf("@every %dm", params.ImageRefreshInterval), func() {
		removeNotRequiredImages()
		refreshImages()
		cleanDangling()
	})

	// State-enforcer
	c.AddFunc("0 * * * * *", func() {
		cleanContainers()
		stopUnexpectedContainers()
		removeDeprecatedContainers()
		startExpectedContainers()
	})

	// Config reload
	c.AddFunc(fmt.Sprintf("@every %dm", params.ConfigLoadInterval), func() {
		reloadConfig(params)
	})
	reloadConfig(params)

	c.Start()

	for {
		select {
		case masterState := <-serfElector.MasterState:
			if masterState {
				// Give the program 60s before taking actions
				remoteActionTimer.Reset(time.Second * 60)
				log.Print("Enabled remote action scheduling")
			} else {
				remoteActionTimer.Stop()
				log.Print("Disabled remote actions scheduling")
			}
		case <-remoteActionTimer.C:
			// TODO: Implement remote action scheduling
		}
	}

}
