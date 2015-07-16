package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/robfig/cron"
	"gopkg.in/yaml.v2"
)

type config map[string]containerConfig

type containerConfig struct {
	Command     []string     `yaml:"command,omitempty"`
	Environment []string     `yaml:"environment,omitempty"`
	Hosts       []string     `yaml:"hosts"`
	Image       string       `yaml:"image"`
	Links       []string     `yaml:"links"`
	Ports       []portConfig `yaml:"ports,omitempty"`
	Tag         string       `yaml:"tag"`
	UpdateTimes []string     `yaml:"update_times,omitempty"`
	Volumes     []string     `yaml:"volumes,omitempty"`
	StartTimes  string       `yaml:"start_times"`
}

type portConfig struct {
	Container string `yaml:"container"`
	Local     string `yaml:"local"`
}

func loadConfig(url string) (*config, error) {
	result := make(config)

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

func readConfig(filename string) (*config, error) {
	result := make(config)

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

func (c containerConfig) shouldBeRunning(hostname string) bool {
	// Not for our host? Nope.
	if !stringInSlice(hostname, c.Hosts) && !stringInSlice("ALL", c.Hosts) {
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
