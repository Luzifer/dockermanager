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
var remoteActionTimer *time.Timer
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

func timeAllowed(allowedTimes []string) bool {
	if len(allowedTimes) == 0 {
		return true
	}

	for _, timeFrame := range allowedTimes {
		times := strings.Split(timeFrame, "-")
		if len(times) != 2 {
			continue
		}

		day := time.Now().Format("2006-01-02")
		timezone := time.Now().Format("-0700")

		t1, et1 := time.Parse("2006-01-02 15:04 -0700", fmt.Sprintf("%s %s %s", day, times[0], timezone))
		t2, et2 := time.Parse("2006-01-02 15:04 -0700", fmt.Sprintf("%s %s %s", day, times[1], timezone))
		if et1 != nil || et2 != nil {
			log.Printf("Timeframe '%s' is invalid. Format is HH:MM-HH:MM", timeFrame)
			continue
		}

		if t2.Before(t1) {
			log.Printf("Timeframe '%s' will never work. Second time has to be bigger.", timeFrame)
		}

		if t1.Before(time.Now()) && t2.After(time.Now()) {
			return true
		}
	}

	return false
}

// #### DOCKER ####

func getImages(dangling bool) []docker.APIImages {
	images, err := dockerClient.ListImages(docker.ListImagesOptions{
		All: false,
	})
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
		Cmd:          cfg.Command,
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
		Links:        cfg.Links,
		Privileged:   false,
		PortBindings: make(map[docker.Port][]docker.PortBinding),
	}

	for _, v := range cfg.Ports {
		s := strings.Split(v.Local, ":")
		hostConfig.PortBindings[docker.Port(v.Container)] = []docker.PortBinding{docker.PortBinding{
			HostIP:   s[0],
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

func getExpectedRunningNames() []string {
	expectedRunning := []string{}
	for name, containerCfg := range *cfg {
		if stringInSlice(serfElector.MyName, containerCfg.Hosts) || stringInSlice("ALL", containerCfg.Hosts) {
			expectedRunning = append(expectedRunning, name)
		}
	}
	return expectedRunning
}

func stopUnexpectedContainers() {
	expectedRunning := getExpectedRunningNames()

	// Stop containers not expected to be running
	currentRunning, err := listRunningContainers()
	orLog(err)
	if err != nil {
		return
	}
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
}

func removeDeprecatedContainers() {
	expectedRunning := getExpectedRunningNames()

	// Stop containers who are of deprecated images
	currentRunning, err := listRunningContainers()
	orLog(err)
	if err != nil {
		return
	}
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
			containerDetails, err := dockerClient.InspectContainer(v.ID)
			orFail(err)

			if stringInSlice(fmt.Sprintf("/%s", n), v.Names) && !strings.HasPrefix(currentImageID, containerDetails.Image) {
				if timeAllowed((*cfg)[n].UpdateTimes) == false {
					log.Printf("Image %s has update but container %s is not allowed to update now.", v.Image, v.ID)
					continue
				}
				log.Printf("Image: %s Current: %s", containerDetails.Image, currentImageID)
				log.Printf("Stopping deprecated container %s", v.ID)
				err := dockerClient.StopContainer(v.ID, 5)
				orFail(err)
				time.Sleep(time.Second * 5)

				log.Printf("Removing deprecated container %s", v.ID)
				dockerClient.RemoveContainer(docker.RemoveContainerOptions{
					ID: v.ID,
				})

				currentRunning, err = listRunningContainers()
				orFail(err)
			}
		}
	}
}

func startExpectedContainers() {
	expectedRunning := getExpectedRunningNames()

	// Start expected containers
	currentRunning, err := listRunningContainers()
	orLog(err)
	if err != nil {
		return
	}
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
	cleanupActionInterval := flag.Int("cleanupInterval", 60, "Sleep time in minutes to wait between cleanup actions")
	configFile := flag.String("configFile", "./config.yaml", "File to load the configuration from")
	configLoadInterval := flag.Int("configInterval", 10, "Sleep time in minutes to wait between config reloads")
	configURL := flag.String("configURL", "", "URL to the config file for direct download (overrides configFile)")
	connectPort := flag.Int("port", 2221, "Port to connect to the docker daemon")
	localActionInterval := flag.Int("localInterval", 10, "Sleep time in minutes to wait between local actions")
	serfAddress := flag.String("serfAddress", "127.0.0.1:7373", "Address of the serf agent to connect to")
	flag.Parse()

	serfElector = newSerfMasterElector()
	go serfElector.Run(*serfAddress)

	// Create a timer for local actions
	actionTimer = time.NewTimer(time.Second * 60)

	// Create a timer but stop it immediately for later usage in remote actions
	remoteActionTimer = time.NewTimer(time.Second * 60)
	remoteActionTimer.Stop()

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
				remoteActionTimer.Reset(time.Second * 60)
				log.Print("Enabled remote action scheduling")
			} else {
				remoteActionTimer.Stop()
				log.Print("Disabled remote actions scheduling")
			}
		case <-actionTimer.C:
			log.Print("Local Action-Tick!")

			refreshImages()
			stopUnexpectedContainers()
			removeDeprecatedContainers()
			startExpectedContainers()

			actionTimer.Reset(time.Minute * time.Duration(*localActionInterval))
		case <-remoteActionTimer.C:
			// TODO: Implement remote action scheduling
		case <-cleanupTimer.C:
			log.Print("Cleanup-Tick!")

			cleanContainers()
			cleanDangling()

			cleanupTimer.Reset(time.Minute * time.Duration(*cleanupActionInterval))
		case <-configTimer.C:
			log.Print("Loading config...")

			var newCfg *config
			if *configURL == "" {
				newCfg, err = readConfig(*configFile)
			} else {
				newCfg, err = loadConfig(*configURL)
			}
			if err == nil {
				cfg = newCfg
			}

			configTimer.Reset(time.Minute * time.Duration(*configLoadInterval))
		}
	}

}
