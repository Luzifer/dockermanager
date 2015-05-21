package main

import "flag"

type dockerManagerParams struct {
	ConfigFile string
	ConfigURL  string

	ConnectPort int
	SerfAddress string

	ConfigLoadInterval   int
	ImageRefreshInterval int
}

func GetStartupParameters() *dockerManagerParams {
	var (
		configFile = flag.String("configFile", "./config.yaml", "File to load the configuration from")
		configURL  = flag.String("configURL", "", "URL to the config file for direct download (overrides configFile)")

		connectPort = flag.Int("port", 2221, "Port to connect to the docker daemon")
		serfAddress = flag.String("serfAddress", "127.0.0.1:7373", "Address of the serf agent to connect to")

		configLoadInterval   = flag.Int("configInterval", 10, "Sleep time in minutes to wait between config reloads")
		imageRefreshInterval = flag.Int("refreshInterval", 30, "fetch new images every <N> minutes")
	)

	flag.Parse()

	return &dockerManagerParams{
		ConfigFile: *configFile,
		ConfigURL:  *configURL,

		ConnectPort: *connectPort,
		SerfAddress: *serfAddress,

		ConfigLoadInterval:   *configLoadInterval,
		ImageRefreshInterval: *imageRefreshInterval,
	}
}
