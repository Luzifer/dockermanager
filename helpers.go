package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

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
