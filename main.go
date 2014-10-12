package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/fsouza/go-dockerclient"
)

var serfElector *serfMasterElector
var actionTimer *time.Timer
var cleanupTimer *time.Timer
var configTimer *time.Timer
var dockerClient *docker.Client
var cfg *config

// #### HELPERS ####

func orFail(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func appendIfMissing(slice []string, s string) []string {
	for _, e := range slice {
		if e == s {
			return slice
		}
	}
	return append(slice, s)
}

// #### DOCKER ####

func getImages(dangling bool) []docker.APIImages {
	images, err := dockerClient.ListImages(false)
	orFail(err)

	response := []docker.APIImages{}

	for _, v := range images {
		if dangling {
			if len(v.RepoTags) == 1 && v.RepoTags[0] == "<none>:<none>" {
				response = append(response, v)
			}
		} else {
			if len(v.RepoTags) != 1 || v.RepoTags[0] != "<none>:<none>" {
				response = append(response, v)
			}
		}
	}

	return response
}

func cleanDangling() {
	images := getImages(true)
	for _, v := range images {
		log.Printf("Removing dangling image: %s", v.ID)
		dockerClient.RemoveImage(v.ID)
	}
}

func refreshImages() {
	for _, v := range *cfg {
		log.Printf("Refreshing repo %s:%s...", v.Image, v.Tag)
		err := dockerClient.PullImage(docker.PullImageOptions{
			Repository: v.Image,
			Tag:        v.Tag,
		}, docker.AuthConfiguration{})
		orFail(err)
	}
}

// #### MAIN ####

func main() {
	serfAddress := flag.String("serfAddress", "127.0.0.1:7373", "Address of the serf agent to connect to")
	//configURL := flag.String("configURL", "", "URL to the config file for direct download")
	connectPort := flag.Int("port", 2221, "Port to connect to the docker daemon")
	flag.Parse()

	serfElector = newSerfMasterElector()
	go serfElector.Run(*serfAddress)

	// Create a timer but stop it immediately for later usage
	actionTimer = time.NewTimer(time.Second * 60)
	actionTimer.Stop()

	// Cleanup is done by each node individually, not only by the master
	cleanupTimer = time.NewTimer(time.Second * 30)

	configTimer = time.NewTimer(time.Second * 1)

	var err error
	dockerClient, err = docker.NewClient(fmt.Sprintf("tcp://127.0.0.1:%d", *connectPort))
	orFail(err)

	for {
		select {
		case masterState := <-serfElector.MasterState:
			if masterState {
				// Give the program 60s before taking actions
				actionTimer.Reset(time.Second * 60)
				log.Print("Enabled actions")
			} else {
				actionTimer.Stop()
				log.Print("Disabled actions")
			}
		case <-actionTimer.C:
			log.Print("Action-Tick!")

			//refreshImages()
			//cleanDangling()

			actionTimer.Reset(time.Second * 300)
		case <-cleanupTimer.C:
			log.Print("Cleanup-Tick!")

			refreshImages()
			cleanDangling()

			cleanupTimer.Reset(time.Minute * 30)
		case <-configTimer.C:
			log.Print("Loading config...")

			cfg = readConfig("./config.yaml")

			configTimer.Reset(time.Minute * 10)
		}
	}

}
