package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/robfig/cron"
	"gopkg.in/yaml.v2"
)

type config map[string]containerConfig

type containerConfig struct {
	Command     []string     `yaml:"command,omitempty" json:"command"`
	Environment []string     `yaml:"environment,omitempty" json:"environment"`
	Hosts       []string     `yaml:"hosts" json:"hosts"`
	Image       string       `yaml:"image" json:"image"`
	Links       []string     `yaml:"links" json:"links"`
	Ports       []portConfig `yaml:"ports,omitempty" json:"ports"`
	Tag         string       `yaml:"tag" json:"tag"`
	UpdateTimes []string     `yaml:"update_times,omitempty" json:"updatetimes"`
	Volumes     []string     `yaml:"volumes,omitempty" json:"volumes"`
	StartTimes  string       `yaml:"start_times" json:"starttimes"`
	StopTimeout uint         `yaml:"stop_timeout" json:"stoptimes"`
}

type portConfig struct {
	Container string `yaml:"container" json:"container"`
	Local     string `yaml:"local" json:"local"`
}

func loadConfigFromURL(url string) (*config, error) {
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

func loadConfigFromFile(filename string) (*config, error) {
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

func (c containerConfig) checksum() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%+x", sha256.Sum256(data)), nil
}
