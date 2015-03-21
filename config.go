package main

import (
	"io/ioutil"
	"log"
	"net/http"

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
