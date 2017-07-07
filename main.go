package main

import (
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/Luzifer/dockermanager/config"
	"github.com/Luzifer/rconfig"
	log "github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
)

var (
	cfg struct { // FIXME: Rename me to "cfg" after removing cfg
		Config   string `default:"config.yaml" flag:"config,c" description:"Config file or URL to read the config from"`
		LogLevel string `flag:"log-level" default:"info" description:"Set log level (debug, info, warning, error)"`

		DockerHost    string `default:"unix:///var/run/docker.sock" flag:"docker-host" env:"DOCKER_HOST" description:"Connection method to the docker server"`
		DockerCertDir string `default:"" flag:"docker-certs" description:"Directory containing cert.pem, key.pem, ca.pem for the registry"`

		ConfigLoadInterval   time.Duration `default:"10m" flag:"configInterval" description:"Sleep time to wait between config reloads"`
		ImageRefreshInterval time.Duration `default:"30m" flag:"refreshInterval" description:"fetch new images every <N>"`

		CleanupTTL time.Duration `flag:"cleanup-ttl" default:"1h" description:"Time to wait until images and containers gets cleaned up"`

		ManageFullHost bool `default:"true" flag:"fullHost" description:"Manage all containers on host"`

		VersionAndExit bool `flag:"version" default:"false" description:"Print version information and exit"`
	}

	dockerClient     *docker.Client
	authConfig       *docker.AuthConfigurations
	configReloadChan = make(chan os.Signal, 1)
	hostname         string

	version = "dev"
)

// #### INIT ####
func init() {
	if err := rconfig.ParseAndValidate(&cfg); err != nil {
		log.Fatalf("Unable to parse CLI flags: %s", err)
	}

	if logLevel, err := log.ParseLevel(cfg.LogLevel); err != nil {
		log.Fatalf("Unable to parse log level: %s", err)
	} else {
		log.SetLevel(logLevel)
	}

	if cfg.VersionAndExit {
		fmt.Printf("dockermanager %s", version)
		os.Exit(0)
	}
}

// #### CONFIG ####
func loadConfig() (config.Config, error) {
	log.Debugf("Loading config...")

	var (
		c   config.Config
		err error
	)
	if _, err = os.Stat(cfg.Config); err == nil {
		c, err = config.LoadConfigFromFile(cfg.Config)
	} else {
		c, err = config.LoadConfigFromURL(cfg.Config)
	}

	if err != nil {
		return c, err
	}

	if _, err := c.GetDependencyChain(); err != nil {
		return c, fmt.Errorf("Calculating the dependency chain caused an error: %s", err)
	}

	return c, nil
}

// #### MAIN ####

func main() {
	var err error
	signal.Notify(configReloadChan, syscall.SIGHUP)

	if hostname, err = os.Hostname(); err != nil {
		log.Fatalf("Unable to determine hostname: %s", err)
	}

	if cfg.DockerCertDir == "" {
		dockerClient, err = docker.NewClient(cfg.DockerHost)
	} else {
		dockerClient, err = docker.NewTLSClient(
			cfg.DockerHost,
			path.Join(cfg.DockerCertDir, "cert.pem"),
			path.Join(cfg.DockerCertDir, "key.pem"),
			path.Join(cfg.DockerCertDir, "ca.pem"),
		)
	}
	if err != nil {
		log.Fatalf("Unable to create Docker client: %s", err)
	}

	// Load local .dockercfg
	authConfig = &docker.AuthConfigurations{}
	auth, err := docker.NewAuthConfigurationsFromDockerCfg()
	if err == nil {
		authConfig = auth
	} else {
		log.Warnf("Could not read authconfig, continuing without authentication: %s", err)
	}

	configFile, err := loadConfig()
	if err != nil {
		log.Fatalf("Initial configuration load failed: %s", err)
	}

	sched, err := newScheduler(hostname, dockerClient, authConfig, configFile, cfg.ImageRefreshInterval)
	if err != nil {
		log.Fatalf("Unable to initialize scheduler: %s", err)
	}

	go func() { log.Fatalf("Scheduler had an error: %s", <-sched.Errors) }()

	if cfg.ManageFullHost {
		sched.EnableImageCleanup(cfg.CleanupTTL)
	}

	// Config reload
	go func() {
		for range time.Tick(cfg.ConfigLoadInterval) {
			configReloadChan <- syscall.SIGHUP
		}
	}()

	for range configReloadChan {
		configFile, err := loadConfig()
		if err != nil {
			log.Errorf("Unable to reload configuration, old one is kept active: %s", err)
			continue
		}
		sched.UpdateConfiguration(configFile)
	}

}
