package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/Luzifer/go_helpers/str"
	"github.com/robfig/cron"
	"gopkg.in/yaml.v2"
)

// Config represents the map of container configurations
type Config map[string]ContainerConfig

// ContainerConfig represents a single container to be started on the specified Hosts
type ContainerConfig struct {
	Command     []string                 `yaml:"command,omitempty" json:"command"`
	Environment []string                 `yaml:"environment,omitempty" json:"environment"`
	Hosts       []string                 `yaml:"hosts" json:"hosts"`
	Image       string                   `yaml:"image" json:"image"`
	Links       []string                 `yaml:"links" json:"links"`
	Ports       []PortConfig             `yaml:"ports,omitempty" json:"ports"`
	Tag         string                   `yaml:"tag" json:"tag"`
	UpdateTimes []string                 `yaml:"update_times,omitempty" json:"updatetimes"`
	Volumes     []string                 `yaml:"volumes,omitempty" json:"volumes"`
	StartTimes  string                   `yaml:"start_times" json:"starttimes"`
	StopTimeout uint                     `yaml:"stop_timeout" json:"stoptimes"`
	Labels      map[string]string        `yaml:"labels" json:"labels"`
	DockerProxy DockerProxyConfiguration `yaml:"dockerproxy" json:"dockerproxy"`
}

// DockerProxyConfiguration contains a slug and a port to map for the dockerproxy
type DockerProxyConfiguration struct {
	Slug string `yaml:"slug" json:"slug"`
	Port int    `yaml:"port" json:"port"`
}

// PortConfig maps container ports to host ports
type PortConfig struct {
	Container string `yaml:"container" json:"container"`
	Local     string `yaml:"local" json:"local"`
}

// LoadConfigFromURL retrieves a Config object from a remote URL
func LoadConfigFromURL(url string) (*Config, error) {
	result := make(Config)

	resp, err := http.Get(url)
	if err != nil {
		log.Print(err)
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	err = yaml.Unmarshal(body, &result)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	return &result, nil
}

// LoadConfigFromFile retrieves a Config object from a local file
func LoadConfigFromFile(filename string) (*Config, error) {
	result := make(Config)

	body, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	err = yaml.Unmarshal(body, &result)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	return &result, nil
}

// ShouldBeRunning determines whether a ContainerConfig object should be started
func (c ContainerConfig) ShouldBeRunning(hostname string, lastStartContainerCall time.Time) bool {
	// Not for our host? Nope.
	if !str.StringInSlice(hostname, c.Hosts) && !str.StringInSlice("ALL", c.Hosts) {
		return false
	}

	// No schedule present? Should definitely be running.
	if c.StartTimes == "" {
		return true
	}

	schedule, err := cron.Parse("0 " + c.StartTimes)
	if err != nil {
		// Warn about invalid schedule but never start this.
		log.Printf("Invalid start_times: %s", err)
		return false
	}

	// Add one second to last try as last try has to be xx:xx:00 and all cronjobs are at that position too
	cmp := lastStartContainerCall.Add(time.Second)
	// Sub one second to have it at xx:xx:59 so we are at least 1s after that point of time
	nxt := schedule.Next(cmp).Add(-1 * time.Second)
	if nxt.Before(time.Now()) {
		return true
	}

	// If we get here, we should probably not be running.
	return false
}

// Checksum generates a hash over the ContainerConfig to compare it to older versions
func (c ContainerConfig) Checksum() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%+x", sha256.Sum256(data)), nil
}
