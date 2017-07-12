package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Luzifer/dockermanager/config"
	"github.com/Luzifer/go_helpers/str"
	docker "github.com/fsouza/go-dockerclient"
	deadlock "github.com/sasha-s/go-deadlock"
	log "github.com/sirupsen/logrus"
)

const (
	imageManagerInterval     = time.Minute
	containerManagerInterval = time.Minute

	lockConfig     = "config"
	lockContainers = "containers"
	lockImages     = "images"
)

var (
	errListenerLoopEnded = errors.New("Listener loop ended")
)

type container struct {
	Checksum    string
	Container   *docker.Container
	IsManaged   bool
	IsScheduled bool
}

type image struct {
	Image           *docker.Image
	LastKnownUpdate time.Time
}

type apiEventHandlerFunction func(*docker.APIEvents) error

func dummyHandler(evt *docker.APIEvents) error {
	//log.Debugf("Received unhandled API event: %#v", evt)
	return nil
}

type scheduler struct {
	Errors chan error

	authConfig           *docker.AuthConfigurations
	cleanupActive        bool
	cleanupMinAge        time.Duration
	client               *docker.Client
	config               config.Config
	hostname             string
	imageRefreshInterval time.Duration
	knownContainers      map[string]container
	knownImages          map[string]image
	listener             chan *docker.APIEvents

	locks map[string]*deadlock.RWMutex
}

func newScheduler(hostname string, client *docker.Client, authConfig *docker.AuthConfigurations, cfg config.Config, imageRefreshInterval time.Duration) (*scheduler, error) {
	s := &scheduler{
		authConfig:           authConfig,
		cleanupActive:        false,
		client:               client,
		config:               cfg,
		hostname:             hostname,
		imageRefreshInterval: imageRefreshInterval,
		knownContainers:      make(map[string]container),
		knownImages:          make(map[string]image),
		listener:             make(chan *docker.APIEvents, 10),

		locks: make(map[string]*deadlock.RWMutex),
	}

	if err := s.collectInitialInformation(); err != nil {
		return nil, err
	}

	if err := s.client.AddEventListener(s.listener); err != nil {
		return nil, err
	}
	go s.listen()

	go s.imageManager()
	go s.containerManager()

	return s, nil
}

func (s *scheduler) listen() {
	for evt := range s.listener {

		log.WithFields(log.Fields{
			"type":   evt.Type,
			"action": evt.Action,
			"actor":  evt.Actor.ID,
		}).Debugf("Event received")

		if hdl, ok := map[string]apiEventHandlerFunction{
			"container": s.handleContainerEvent,
			"image":     s.handleImageEvent,
			"network":   dummyHandler,
			"volume":    dummyHandler,
		}[evt.Type]; ok {
			if err := hdl(evt); err != nil {
				log.Errorf("Unable to handle %s event: %s", evt.Type, err)
			}
		}
	}

	// If we are ending here the channel was closed.
	s.Errors <- errListenerLoopEnded
}

/* Public Interface */

func (s *scheduler) UpdateConfiguration(cfg config.Config) {
	s.lock(lockConfig, true)
	defer s.unlock(lockConfig, true)

	s.config = cfg
}

func (s *scheduler) EnableImageCleanup(minAge time.Duration) {
	s.cleanupMinAge = minAge
	s.cleanupActive = true
}

/* Private Interface */

func (s *scheduler) lock(topic string, rw bool) {
	if _, ok := s.locks[topic]; !ok {
		s.locks[topic] = new(deadlock.RWMutex)
	}

	if rw {
		s.locks[topic].Lock()
	} else {
		s.locks[topic].RLock()
	}
}

func (s *scheduler) unlock(topic string, rw bool) {
	if _, ok := s.locks[topic]; !ok {
		s.locks[topic] = new(deadlock.RWMutex)
	}

	if rw {
		s.locks[topic].Unlock()
	} else {
		s.locks[topic].RUnlock()
	}
}

func (s *scheduler) collectInitialInformation() error {
	imgs, err := s.client.ListImages(docker.ListImagesOptions{
		All: true,
	})
	if err != nil {
		return fmt.Errorf("Unable to list images: %s", err)
	}

	for _, i := range imgs {
		if err = s.refreshImageInformation(i.ID, false); err != nil {
			return fmt.Errorf("Unable to fetch image information for %q: %s", i.ID, err)
		}
	}

	conts, err := s.client.ListContainers(docker.ListContainersOptions{
		All: true,
	})
	if err != nil {
		return fmt.Errorf("Unable to list containers: %s", err)
	}

	for _, c := range conts {
		if err := s.refreshContainerInformation(c.ID, false); err != nil {
			return fmt.Errorf("Unable to fetch container information for %q: %s", c.ID, err)
		}
	}

	return nil
}

func (s *scheduler) refreshImageInformation(id string, remove bool) error {
	if remove {
		s.lock(lockImages, true)
		defer s.unlock(lockImages, true)
		delete(s.knownImages, id)
		return nil
	}

	img, err := s.client.InspectImage(id)
	if err != nil {
		return fmt.Errorf("Unable to inspect image %q: %s", id, err)
	}

	s.lock(lockImages, true)
	defer s.unlock(lockImages, true)
	s.knownImages[img.ID] = image{
		Image:           img,
		LastKnownUpdate: time.Now(),
	}

	return nil
}

func (s *scheduler) refreshContainerInformation(id string, remove bool) error {
	if remove {
		s.lock(lockContainers, true)
		defer s.unlock(lockContainers, true)
		delete(s.knownContainers, id)
		return nil
	}

	cont, err := s.client.InspectContainer(id)
	if err != nil {
		return fmt.Errorf("Unable to inspect container %q: %s", id, err)
	}

	c := container{
		Container: cont,
	}

	_, c.IsManaged = cont.Config.Labels[labelIsManaged]
	_, c.IsScheduled = cont.Config.Labels[labelIsScheduled]
	c.Checksum = cont.Config.Labels[labelConfigHash]

	s.lock(lockContainers, true)
	defer s.unlock(lockContainers, true)
	s.knownContainers[cont.ID] = c

	return nil
}

func (s *scheduler) getContainerByName(name string) *docker.Container {
	s.lock(lockContainers, false)
	defer s.unlock(lockContainers, false)

	for _, cont := range s.knownContainers {
		if strings.TrimLeft(cont.Container.Name, "/") == name {
			return cont.Container
		}
	}

	return nil
}

func (s *scheduler) getImageByName(name string) *docker.Image {
	s.lock(lockImages, false)
	defer s.unlock(lockImages, false)

	for _, img := range s.knownImages {
		if str.StringInSlice(name, img.Image.RepoTags) {
			return img.Image
		}
	}

	return nil
}

// Handler definitions

func (s *scheduler) handleContainerEvent(evt *docker.APIEvents) error {
	if hdl, ok := map[string]apiEventHandlerFunction{
		"add":         dummyHandler,                                                                                    // FIXME: What's this?
		"attach":      dummyHandler,                                                                                    // No need to handle
		"commit":      dummyHandler,                                                                                    // No need to handle
		"copy":        dummyHandler,                                                                                    // FIXME: What's this?
		"create":      func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"destroy":     func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, true) },  // Actor.ID is the ID of the container
		"die":         func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"exec_create": dummyHandler,                                                                                    // No need to handle
		"exec_start":  dummyHandler,                                                                                    // No need to handle
		"export":      dummyHandler,                                                                                    // No need to handle
		"kill":        func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"oom":         func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"pause":       func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"rename":      func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"resize":      func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"restart":     func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"start":       func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"stop":        func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"top":         dummyHandler,                                                                                    // FIXME: What's this?
		"unpause":     func(evt *docker.APIEvents) error { return s.refreshContainerInformation(evt.Actor.ID, false) }, // Actor.ID is the ID of the container
		"update":      dummyHandler,                                                                                    // FIXME: What's this?
	}[evt.Action]; ok {
		return hdl(evt)
	}
	return nil
}

func (s *scheduler) handleImageEvent(evt *docker.APIEvents) error {
	if hdl, ok := map[string]apiEventHandlerFunction{
		"delete": func(evt *docker.APIEvents) error { return s.refreshImageInformation(evt.Actor.ID, true) },  // Actor.ID is the ID (sha256:...) of the image
		"import": dummyHandler,                                                                                // FIXME: This needs to be handled
		"pull":   func(evt *docker.APIEvents) error { return s.refreshImageInformation(evt.Actor.ID, false) }, // Actor.ID is the NAME of the image
		"push":   dummyHandler,                                                                                // No need to handle
		"tag":    func(evt *docker.APIEvents) error { return s.refreshImageInformation(evt.Actor.ID, false) }, // Actor.ID is the ID (sha256:...) of the image
		"untag":  func(evt *docker.APIEvents) error { return s.refreshImageInformation(evt.Actor.ID, false) }, // Actor.ID is the ID (sha256:...) of the image
	}[evt.Action]; ok {
		return hdl(evt)
	}
	return nil
}

func (s *scheduler) imageManager() {
	for range time.Tick(imageManagerInterval) {

		s.lock(lockImages, false)
		for id, img := range s.knownImages {

			myName := ""
			for _, t := range img.Image.RepoTags {
				if str.StringInSlice(t, s.config.GetImageList()) {
					myName = t
				}
			}

			if s.cleanupActive {
				if myName == "" && img.Image.Created.Add(s.cleanupMinAge).Before(time.Now()) {
					imageName := id
					if len(img.Image.RepoTags) > 0 {
						imageName = img.Image.RepoTags[0]
					}
					log.Debugf("Image %q is not expected to be there and is %s old, removing...", imageName, time.Since(img.Image.Created))
					if err := s.client.RemoveImage(id); err != nil {
						log.Errorf("Unable to delete image %q: %s", id, err)
					}
					continue
				}
			}

			limit := make(chan struct{}, 10)
			if myName != "" && img.LastKnownUpdate.Add(s.imageRefreshInterval).Before(time.Now()) {
				limit <- struct{}{}
				log.Debugf("Refreshing image %q...", myName)
				go func(myName string, limit chan struct{}) {
					pullImage(docker.ParseRepositoryTag(myName))
					<-limit
				}(myName, limit)
			}

		}
		s.unlock(lockImages, false)

	}
}

func (s *scheduler) containerManager() {
	for range time.Tick(containerManagerInterval) {

		s.removeDeadContainers()
		s.stopUnexpectedContainers()
		s.stopContainersWithUpdates()
		s.startContainers()

	}
}

func (s *scheduler) removeDeadContainers() {
	s.lock(lockContainers, false)
	defer s.unlock(lockContainers, false)

	for id, cont := range s.knownContainers {
		if cont.Container.State.Running {
			// Not dead yet, Jim
			continue
		}

		if cont.Container.Created.Add(s.cleanupMinAge).After(time.Now()) ||
			cont.Container.State.FinishedAt.Add(s.cleanupMinAge).After(time.Now()) {
			// Newly created or newly deceased, don't burry yet
			continue
		}

		if !cont.IsManaged && !cont.IsScheduled && !s.cleanupActive {
			// Not one of ours, no permission to cleanup
			continue
		}

		if _, ok := s.config[strings.TrimLeft(cont.Container.Name, "/")]; ok {
			// Container is still managed, remove will be done by startContainers
			// This is to prevent two simultaneous remove calls which causes trouble
			continue
		}

		if err := s.client.RemoveContainer(docker.RemoveContainerOptions{
			ID: id,
		}); err != nil {
			log.Errorf("Unable to remove container %q: %s", cont.Container.Name, err)
		}
	}
}

func (s *scheduler) stopUnexpectedContainers() {
	s.lock(lockContainers, false)
	defer s.unlock(lockContainers, false)

	for id, cont := range s.knownContainers {
		if !cont.Container.State.Running {
			// It's already dead
			continue
		}

		if !cont.IsManaged && !s.cleanupActive {
			// Not ours, not the police
			continue
		}

		if cont.IsScheduled {
			// Scheduled job, should end itself
			continue
		}

		if _, ok := s.config[strings.TrimLeft(cont.Container.Name, "/")]; !ok {
			// We don't have a config for this one so lets ask it to stop
			go func(id string, cont container) {
				if err := s.client.StopContainer(id, 30); err != nil {
					log.Errorf("Unable to stop container %q: %s", cont.Container.Name, err)
				}
			}(id, cont)
		}
	}
}

func (s *scheduler) stopContainersWithUpdates() {
	s.lock(lockContainers, false)
	defer s.unlock(lockContainers, false)

	for id, cont := range s.knownContainers {
		if !cont.Container.State.Running {
			// It's already dead
			continue
		}

		ccfg, ok := s.config[strings.TrimLeft(cont.Container.Name, "/")]
		if !ok {
			// We don't know about this one, not our job
			continue
		}

		if allowed, err := ccfg.UpdateAllowedAt(time.Now()); err == nil && !allowed {
			// We may not update now, don't bother
			continue
		} else if err != nil {
			log.Errorf("Could not determine whether update is allowed for %q: %s", cont.Container.Name, err)
			continue
		}

		stopIt := false

		if cs, err := ccfg.Checksum(); err == nil && cont.Checksum != "" && cs != cont.Checksum {
			// Checksum mismatch: Ask it to go
			log.Infof("Container %s has a configuration update.", cont.Container.Name)
			stopIt = true
		}

		if img := s.getImageByName(ccfg.Image + ":" + ccfg.Tag); img != nil && img.ID != cont.Container.Image {
			// Image was renewed: Ask it to go
			log.Infof("Container %s has a new image version.", cont.Container.Name)
			stopIt = true
		}

		if stopIt {
			go func(id string, ccfg *config.ContainerConfig, cont container) {
				if err := s.stopContainerGraph(strings.TrimLeft(cont.Container.Name, "/"), true); err != nil {
					log.Errorf("Unable to stop container %q: %s", cont.Container.Name, err)
				}
			}(id, ccfg, cont)
		}
	}
}

func (s *scheduler) stopContainerGraph(name string, isBaseLevel bool) error {
	if isBaseLevel {
		// Only aquire one lock on the config to prevent deadlocks
		s.lock(lockConfig, false)
		defer s.unlock(lockConfig, false)
	}

	ccfg, ok := s.config[name]
	if !ok {
		return fmt.Errorf("No container configuration found")
	}

	dependingOnMe := []string{}
	for n, c := range s.config {
		if str.StringInSlice(name, c.GetDependencies()) {
			dependingOnMe = append(dependingOnMe, n)
		}
	}

	for _, d := range dependingOnMe {
		if err := s.stopContainerGraph(d, false); err != nil {
			return err
		}
	}

	s.lock(lockContainers, false)
	cont := s.getContainerByName(name)
	s.unlock(lockContainers, false)

	stopTime := uint(math.Max(5, float64(ccfg.StopTimeout)))
	return s.client.StopContainer(cont.ID, stopTime)
}

func (s *scheduler) startContainers() {
	s.lock(lockConfig, false)
	defer s.unlock(lockConfig, false)

	chain, err := s.config.GetDependencyChain()
	if err != nil {
		log.Errorf("Unable to get dependency chain: %s", err)
		return
	}

	for _, name := range chain {
		ccfg := s.config[name]

		if !ccfg.ShouldBeRunning(s.hostname) {
			// Should not be running, so don't touch it
			continue
		}

		if cont := s.getContainerByName(name); cont != nil && cont.State.Running {
			// Is already running
			continue
		} else if cont != nil && !cont.State.Running {
			// Isn't running but still known and should be running so remove the old one
			if err := s.client.RemoveContainer(docker.RemoveContainerOptions{
				ID: cont.ID,
			}); err != nil {
				log.Errorf("Unable to remove container %q: %s", cont.Name, err)
				continue
			}
		}

		// Should be running and old versions were removed: Lets start stuff!
		if err := bootContainer(name, ccfg); err != nil {
			log.Errorf("Unable to execute container %q: %s", name, err)
			continue
		}

		if err := ccfg.UpdateNextRun(); err != nil {
			log.Errorf("Unable to update next run for container %q: %s", name, err)
		}
	}
}
