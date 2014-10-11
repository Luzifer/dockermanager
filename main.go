package main

import (
	"flag"
	"log"
	"time"
)

var serfElector *serfMasterElector
var actionTimer *time.Ticker
var actionTimerChan <-chan time.Time

func main() {
	serfAddress := flag.String("serfAddress", "127.0.0.1:7373", "Address of the serf agent to connect to")
	flag.Parse()

	serfElector = newSerfMasterElector()
	go serfElector.Run(*serfAddress)

	serfElector.ConfigVersion = 1

	for {
		select {
		case masterState := <-serfElector.MasterState:
			if masterState {
				actionTimer = time.NewTicker(time.Second * 60)
				actionTimerChan = actionTimer.C
				log.Print("Enabled actions")
			} else {
				actionTimer.Stop()
				log.Print("Disabled actions")
			}
		case <-actionTimerChan:
			log.Print("Action-Tick!")
		}
	}

}
