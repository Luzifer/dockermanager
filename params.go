package main

import "github.com/Luzifer/rconfig"

type dockerManagerParams struct {
	Config string `default:"config.yaml" flag:"config,c" description:"Config file or URL to read the config from"`

	DockerHost    string `default:"tcp://127.0.0.1:2221" flag:"docker-host" env:"DOCKER_HOST" description:"Connection method to the docker server"`
	DockerCertDir string `default:"" flag:"docker-certs" description:"Directory containing cert.pem, key.pem, ca.pem for the registry"`

	ConfigLoadInterval   int `default:"10" flag:"configInterval" description:"Sleep time in minutes to wait between config reloads"`
	ImageRefreshInterval int `default:"30" flag:"refreshInterval" description:"fetch new images every <N> minutes"`

	ManageFullHost bool `default:"true" flag:"fullHost" description:"Manage all containers on host"`
}

func getStartupParameters() *dockerManagerParams {
	cfg := &dockerManagerParams{}

	rconfig.Parse(cfg)

	return cfg
}
