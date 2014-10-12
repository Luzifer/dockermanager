package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
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

// Wrapper to replace the usual error check with fatal logging
func orFail(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// Wrapper to replace the usual error check with logging
func orLog(err error) {
	if err != nil {
		log.Print(err)
	}
}

// appendIfMissing adds a string to a slice when it's not present yet
func appendIfMissing(slice []string, s string) []string {
	for _, e := range slice {
		if e == s {
			return slice
		}
	}
	return append(slice, s)
}

// stringInSlice checks for the existence of a string in the slice
func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// #### DOCKER ####

func getImages(dangling bool) []docker.APIImages {
	images, err := dockerClient.ListImages(false)
	orFail(err)

	nowSeconds := time.Now().Unix()

	response := []docker.APIImages{}

	for _, v := range images {
		if dangling {
			if len(v.RepoTags) == 1 && v.RepoTags[0] == "<none>:<none>" && (nowSeconds-v.Created) > 3600 {
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
		orLog(err)
	}
}

func bootContainer(name string, cfg containerConfig) {
	newcfg := &docker.Config{
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Image:        strings.Join([]string{cfg.Image, cfg.Tag}, ":"),
		Env:          cfg.Environment,
	}
	log.Printf("Creating container %s", name)
	container, err := dockerClient.CreateContainer(docker.CreateContainerOptions{
		Name:   name,
		Config: newcfg,
	})
	orLog(err)
	if err != nil {
		return
	}

	hostConfig := docker.HostConfig{
		Binds:        cfg.Volumes,
		Privileged:   false,
		PortBindings: make(map[docker.Port][]docker.PortBinding),
	}

	for _, v := range cfg.Ports {
		s := strings.Split(v.Local, ":")
		hostConfig.PortBindings[docker.Port(v.Container)] = []docker.PortBinding{docker.PortBinding{
			HostIp:   s[0],
			HostPort: s[1],
		}}
	}

	log.Printf("Starting container %s", container.Name)
	dockerClient.StartContainer(container.Name, &hostConfig)
}

func listRunningContainers() ([]docker.APIContainers, error) {
	containers, err := dockerClient.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	orLog(err)
	return containers, err
}

func ensureContainers() {
	// TODO: Refactor, too long.
	expectedRunning := []string{}
	currentRunning, err := listRunningContainers()
	orLog(err)
	if err != nil {
		return
	}
	for name, containerCfg := range *cfg {
		if stringInSlice(serfElector.MyName, containerCfg.Hosts) || stringInSlice("ALL", containerCfg.Hosts) {
			expectedRunning = append(expectedRunning, name)
		}
	}

	// Stop containers not expected to be running
	for _, v := range currentRunning {
		allowed := false
		for _, n := range v.Names {
			if stringInSlice(strings.Trim(n, "/"), expectedRunning) {
				allowed = true
			}
		}
		if !allowed {
			log.Printf("Stopping container %s as it is not expected to be running.", v.ID)
			err := dockerClient.StopContainer(v.ID, 5)
			orFail(err)
			time.Sleep(time.Second * 5)
		}
	}

	// Stop containers who are of deprecated images
	currentRunning, _ = listRunningContainers()
	images := getImages(false)
	for _, n := range expectedRunning {
		repoName := strings.Join([]string{(*cfg)[n].Image, (*cfg)[n].Tag}, ":")
		currentImageID := "0"
		for _, i := range images {
			if stringInSlice(repoName, i.RepoTags) {
				currentImageID = i.ID
			}
		}
		if currentImageID == "0" {
			log.Printf("Found no image ID for repo %s", strings.Join([]string{(*cfg)[n].Image, (*cfg)[n].Tag}, ":"))
			continue
		}
		for _, v := range currentRunning {
			if stringInSlice(fmt.Sprintf("/%s", n), v.Names) && !strings.HasPrefix(currentImageID, v.Image) && v.Image != repoName {
				log.Printf("Image: %s Current: %s", v.Image, currentImageID)
				log.Printf("Stopping deprecated container %s", v.ID)
				err := dockerClient.StopContainer(v.ID, 5)
				orFail(err)
				time.Sleep(time.Second * 5)
				log.Printf("Removing deprecated container %s", v.ID)
				dockerClient.RemoveContainer(docker.RemoveContainerOptions{
					ID: v.ID,
				})
			}
		}
	}

	// Start expected containers
	currentRunning, _ = listRunningContainers()
	runningNames := []string{}
	for _, v := range currentRunning {
		for _, n := range v.Names {
			runningNames = append(runningNames, strings.Trim(n, "/"))
		}
	}
	for _, n := range expectedRunning {
		if !stringInSlice(n, runningNames) {
			bootContainer(n, (*cfg)[n])
		}
	}
}

func cleanContainers() {
	runningContainers, err := dockerClient.ListContainers(docker.ListContainersOptions{
		All: true,
	})
	orLog(err)
	if err != nil {
		return
	}

	for _, v := range runningContainers {
		if strings.HasPrefix(v.Status, "Exited") {
			log.Printf("Removing stopped container %s", v.ID)
			err := dockerClient.RemoveContainer(docker.RemoveContainerOptions{
				ID: v.ID,
			})
			orLog(err)
		}
	}
}

// #### MAIN ####

func main() {
	serfAddress := flag.String("serfAddress", "127.0.0.1:7373", "Address of the serf agent to connect to")
	configURL := flag.String("configURL", "", "URL to the config file for direct download (overrides configFile)")
	configFile := flag.String("configFile", "./config.yaml", "File to load the configuration from")
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

			refreshImages()
			ensureContainers()

			actionTimer.Reset(time.Second * 300)
		case <-cleanupTimer.C:
			log.Print("Cleanup-Tick!")

			cleanContainers()
			cleanDangling()

			cleanupTimer.Reset(time.Minute * 30)
		case <-configTimer.C:
			log.Print("Loading config...")

			if *configURL == "" {
				cfg = readConfig(*configFile)
			} else {
				cfg = loadConfig(*configURL)
			}

			configTimer.Reset(time.Minute * 10)
		}
	}

}
