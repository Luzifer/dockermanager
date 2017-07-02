package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/Luzifer/dockermanager/config"
	"github.com/fsouza/go-dockerclient"
	"github.com/robfig/cron"
)

var (
	actionTimer      *time.Timer
	cleanupTimer     *time.Timer
	configTimer      *time.Timer
	dockerClient     *docker.Client
	cfg              *config.Config
	authConfig       *docker.AuthConfigurations
	params           *dockerManagerParams
	configReloadChan = make(chan os.Signal, 1)
	hostname         string
)

// #### CONFIG ####
func reloadConfig(params *dockerManagerParams) {
	log.Print("Loading config...")
	var loadErr error

	var newCfg *config.Config

	if _, err := os.Stat(params.Config); err == nil {
		newCfg, loadErr = config.LoadConfigFromFile(params.Config)
	} else {
		newCfg, loadErr = config.LoadConfigFromURL(params.Config)
	}
	if loadErr == nil {
		cfg = newCfg
	}
}

// #### MAIN ####

func main() {
	params = getStartupParameters()

	signal.Notify(configReloadChan, syscall.SIGHUP)

	var err error
	if hostname, err = os.Hostname(); err != nil {
		log.Fatalf("Unable to determine hostname: %s", err)
	}

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
		configReloadChan <- syscall.SIGHUP
	})
	configReloadChan <- syscall.SIGHUP

	c.Start()

	for range configReloadChan {
		reloadConfig(params)
	}

}
