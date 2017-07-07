package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/Luzifer/go_helpers/str"
	"github.com/cnf/structhash"
	"github.com/robfig/cron"
	"gopkg.in/yaml.v2"
)

// Config represents the map of container configurations
type Config map[string]*ContainerConfig

// ContainerConfig represents a single container to be started on the specified Hosts
type ContainerConfig struct {
	Command         []string          `yaml:"command,omitempty" json:"command"`
	Environment     []string          `yaml:"environment,omitempty" json:"environment"`
	Hosts           []string          `yaml:"hosts" json:"hosts"`
	Image           string            `yaml:"image" json:"image"`
	Links           []string          `yaml:"links" json:"links"`
	Ports           []PortConfig      `yaml:"ports,omitempty" json:"ports"`
	Tag             string            `yaml:"tag" json:"tag"`
	UpdateTimes     []string          `yaml:"update_times,omitempty" json:"updatetimes"`
	Volumes         []string          `yaml:"volumes,omitempty" json:"volumes"`
	StartTimes      string            `yaml:"start_times" json:"starttimes"`
	StopTimeout     uint              `yaml:"stop_timeout" json:"stoptimes"`
	Labels          map[string]string `yaml:"labels" json:"labels"`
	AddCapabilities []string          `yaml:"cap_add" json:"cap_add"`

	nextRun *time.Time
}

// PortConfig maps container ports to host ports
type PortConfig struct {
	Container string `yaml:"container" json:"container"`
	Local     string `yaml:"local" json:"local"`
}

// LoadConfigFromURL retrieves a Config object from a remote URL
func LoadConfigFromURL(url string) (Config, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Unable to fetch config from URL: %s", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Unable to read HTTP body: %s", err)
	}

	return parseConfig(body)
}

// LoadConfigFromFile retrieves a Config object from a local file
func LoadConfigFromFile(filename string) (Config, error) {
	body, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("Unable to read config from file %q: %s", filename, err)
	}

	return parseConfig(body)
}

func parseConfig(body []byte) (Config, error) {
	result := make(Config)

	err := yaml.Unmarshal(body, &result)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse config: %s", err)
	}

	for k := range result {
		if err := result[k].UpdateNextRun(); err != nil {
			return nil, fmt.Errorf("Unable to update next run: %s", err)
		}
	}

	return result, nil
}

func (c *ContainerConfig) UpdateNextRun() error {
	if c.StartTimes == "" {
		c.nextRun = nil
		return nil
	}

	schedule, err := cron.Parse("0 " + c.StartTimes)
	if err != nil {
		return fmt.Errorf("Invalid start_times %q: %s", c.StartTimes, err)
	}

	nxt := schedule.Next(time.Now())
	c.nextRun = &nxt

	return nil
}

// ShouldBeRunning determines whether a ContainerConfig object should be started
func (c ContainerConfig) ShouldBeRunning(hostname string) bool {
	// Not for our host? Nope.
	if !str.StringInSlice(hostname, c.Hosts) && !str.StringInSlice("ALL", c.Hosts) {
		return false
	}

	return c.nextRun == nil || c.nextRun.Before(time.Now())
}

// Checksum generates a hash over the ContainerConfig to compare it to older versions
func (c ContainerConfig) Checksum() (string, error) {
	return fmt.Sprintf("%x", structhash.Sha1(c, 1)), nil
}

// UpdateAllowedAt checks whether a container may be updated at the given time
func (c ContainerConfig) UpdateAllowedAt(pit time.Time) (bool, error) {
	if len(c.UpdateTimes) == 0 {
		return true, nil
	}

	for _, timeFrame := range c.UpdateTimes {
		times := strings.Split(timeFrame, "-")
		if len(times) != 2 {
			continue
		}

		day := pit.Format("2006-01-02")
		timezone := pit.Format("-0700")

		t1, et1 := time.Parse("2006-01-02 15:04 -0700", fmt.Sprintf("%s %s %s", day, times[0], timezone))
		t2, et2 := time.Parse("2006-01-02 15:04 -0700", fmt.Sprintf("%s %s %s", day, times[1], timezone))
		if et1 != nil || et2 != nil {
			return false, fmt.Errorf("Timeframe '%s' is invalid. Format is HH:MM-HH:MM", timeFrame)
		}

		if t1.Before(pit) && t2.After(pit) {
			return true, nil
		}
	}

	return false, nil
}

func (c ContainerConfig) GetDependencies() []string {
	deps := []string{}

	// Introduce all linked containers as dependencies
	for _, lnk := range c.Links {
		parts := strings.Split(lnk, ":")
		if len(parts) == 2 {
			deps = append(deps, parts[0])
		}
	}

	return deps
}

func (c Config) GetDependencyChain() ([]string, error) {
	chain := []string{}

	// Run as long as not all elements are in the chain
	for len(chain) < len(c) {

		// Detect whether we changed something
		iterationChangedChain := false
		for name, cfg := range c {
			// Already in the chain? Skip it.
			if str.StringInSlice(name, chain) {
				continue
			}

			deps := cfg.GetDependencies()
			if len(deps) == 0 {
				// Doesn't has dependencies? Great, off to the chain!
				chain = append(chain, name)
				iterationChangedChain = true
				continue
			}

			// Look how many of its dependencies are fulfilled
			resolvedDeps := []string{}
			for _, d := range deps {
				if str.StringInSlice(d, chain) {
					resolvedDeps = append(resolvedDeps, d)
				}
			}

			// All of them fulfilled? Great, off to the chain!
			if len(resolvedDeps) == len(deps) {
				chain = append(chain, name)
				iterationChangedChain = true
				continue
			}
		}

		// Didn't manage to add something to the chain? Hello circle!
		if !iterationChangedChain {
			return nil, errors.New("Detected cyclic dependency")
		}

	}

	return chain, nil
}

func (c Config) GetImageList() []string {
	images := []string{}

	for _, cont := range c {
		images = append(images, fmt.Sprintf("%s:%s", cont.Image, cont.Tag))
	}

	return images
}
