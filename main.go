package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/robfig/cron"
)

var serfElector *serfMasterElector
var actionTimer *time.Timer
var remoteActionTimer *time.Timer
var cleanupTimer *time.Timer
var configTimer *time.Timer
var dockerClient *docker.Client
var cfg *config
var authConfig *docker.AuthConfigurations

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
		auth := docker.AuthConfiguration{}

		reginfo := strings.SplitN(v.Image, "/", 2)
		if len(reginfo) == 2 {
			for s, a := range authConfig.Configs {
				if strings.Contains(s, fmt.Sprintf("://%s/", reginfo[0])) {
					auth = a
				}
			}
		}

		log.Printf("Refreshing repo %s:%s...", v.Image, v.Tag)
		err := dockerClient.PullImage(docker.PullImageOptions{
			Repository: v.Image,
			Tag:        v.Tag,
		}, auth)
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

// #### CONFIG ####
func reloadConfig(params *dockerManagerParams) {
	log.Print("Loading config...")
	var err error

	var newCfg *config
	if params.ConfigURL == "" {
		newCfg, err = readConfig(params.ConfigFile)
	} else {
		newCfg, err = loadConfig(params.ConfigURL)
	}
	if err == nil {
		cfg = newCfg
	}
}

// #### MAIN ####

func main() {
	params := GetStartupParameters()

	serfElector = newSerfMasterElector()
	go serfElector.Run(params.SerfAddress)

	// Create a timer but stop it immediately for later usage in remote actions
	remoteActionTimer = time.NewTimer(time.Second * 60)
	remoteActionTimer.Stop()

	var err error
	dockerClient, err = docker.NewClient(fmt.Sprintf("tcp://127.0.0.1:%d", params.ConnectPort))
	orFail(err)

	// Load local .dockercfg
	authConfig = &docker.AuthConfigurations{}
	auth, err := docker.NewAuthConfigurationsFromDockerCfg()
	if err == nil {
		authConfig = auth
	} else {
		log.Printf("Could not read authconfig: %s\n", err)
	}

	c := cron.New()

	// Refresh images
	c.AddFunc(fmt.Sprintf("@every %dm", params.ImageRefreshInterval), func() {
		refreshImages()
		cleanDangling()
	})

	// State-enforcer
	c.AddFunc("@every 1m", func() {
		cleanContainers()
		stopUnexpectedContainers()
		removeDeprecatedContainers()
		startExpectedContainers()
	})

	// Config reload
	c.AddFunc(fmt.Sprintf("@every %dm", params.ConfigLoadInterval), func() {
		reloadConfig(params)
	})
	reloadConfig(params)

	c.Start()

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
		case <-remoteActionTimer.C:
			// TODO: Implement remote action scheduling
		}
	}

}
