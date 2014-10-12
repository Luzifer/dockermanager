package main

import (
	"log"
	"sort"
	"time"

	"github.com/gonuts/binary"
	"github.com/hashicorp/serf/client"
)

type serfMasterElector struct {
	ConfigVersion int64
	MasterState   <-chan bool // Submits the current master state as soon as it changes
	iAmMaster     bool
	masterState   chan bool
	members       []memberNode
	MyName        string
	myStart       int64
	serfClient    *client.RPCClient
}

type memberInfo struct {
	ConfigVersion int64
	Name          string
	StartTime     int64
}

type memberNode struct {
	LastContact time.Time
	Info        *memberInfo
}

type byStartTime []memberNode

func (bst byStartTime) Len() int      { return len(bst) }
func (bst byStartTime) Swap(i, j int) { bst[i], bst[j] = bst[j], bst[i] }
func (bst byStartTime) Less(i, j int) bool {
	if bst[i].Info.ConfigVersion < bst[j].Info.ConfigVersion {
		// If i has an older config version than j rank it below
		return false
	} else if bst[i].Info.ConfigVersion > bst[j].Info.ConfigVersion {
		// If the config version is bigger, rank it up
		return true
	}
	// If the config version is identical, bigger uptime (=lesser start time) wins
	return bst[i].Info.StartTime < bst[j].Info.StartTime
}

func newSerfMasterElector() *serfMasterElector {
	stateChan := make(chan bool, 1)
	return &serfMasterElector{
		ConfigVersion: 0,
		MasterState:   stateChan,
		iAmMaster:     false,
		masterState:   stateChan,
	}
}

func (s *serfMasterElector) marshal(v interface{}) []byte {
	res, err := binary.Marshal(v)
	if err != nil {
		log.Fatal(err)
	}
	return res
}

func (s *serfMasterElector) unmarshal(b []byte, v interface{}) {
	err := binary.Unmarshal(b, v)
	if err != nil {
		log.Fatal(err)
	}
}

func (s *serfMasterElector) handleMasterElectionMessage(info *memberInfo) {
	member := memberNode{
		Info:        info,
		LastContact: time.Now(),
	}
	s.members = append(s.members, member)
	s.doMasterElection()
}

func (s *serfMasterElector) handleMemberQuitMessage(member string) {
	newMembers := []memberNode{}
	for _, v := range s.members {
		if v.Info.Name != member {
			newMembers = append(newMembers, v)
		}
	}
	s.members = newMembers
	s.doMasterElection()
}

func (s *serfMasterElector) doMasterElection() {
	newMembers := []memberNode{}
	for _, v := range s.members {
		if time.Now().Sub(v.LastContact) < time.Second*15 {
			newMembers = append(newMembers, v)
		}
	}
	s.members = newMembers

	sort.Sort(byStartTime(s.members))
	newState := s.members[0].Info.Name == s.MyName
	if newState != s.iAmMaster {
		s.masterState <- newState
	}
	s.iAmMaster = newState
	log.Printf("AmIMaster? %v", s.iAmMaster)
}

func (s *serfMasterElector) Run(serfAddress string) {
	s.myStart = time.Now().Unix()

	serfClient, err := client.NewRPCClient(serfAddress)
	if err != nil {
		log.Fatal(err)
	}

	msgChan := make(chan map[string]interface{}, 5)
	tick := time.NewTicker(time.Second * 10).C

	serfClient.Stream("", msgChan)

	stats, _ := serfClient.Stats()
	s.MyName = stats["agent"]["name"]

	defer serfClient.UserEvent("MemberQuit", s.marshal(s.MyName), false)

	for {
		select {
		case msg := <-msgChan:
			switch {
			case msg["Event"] == "user" && msg["Name"] == "MasterElection":
				payload := memberInfo{}
				s.unmarshal(msg["Payload"].([]byte), &payload)
				log.Printf("MasterElection: %s %s", payload.Name, payload.StartTime)
				s.handleMasterElectionMessage(&payload)
			case msg["Event"] == "user" && msg["Name"] == "MemberQuit":
				var name string
				s.unmarshal(msg["Payload"].([]byte), &name)
				s.handleMemberQuitMessage(name)
			default:
				log.Printf("Message: %q\n", msg)
			}
		case <-tick:
			serfClient.UserEvent("MasterElection", s.marshal(&memberInfo{
				ConfigVersion: s.ConfigVersion,
				Name:          s.MyName,
				StartTime:     s.myStart,
			}), false)
		}
	}

	err = serfClient.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func (s *serfMasterElector) IsMaster() bool {
	return s.iAmMaster
}
