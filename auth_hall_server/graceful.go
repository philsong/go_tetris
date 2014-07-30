package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// gracefully exit the program
func initGraceful() {
	go notify()
}

var allGSReleased = false
var progCanExit = false

func notify() {
	sigs := make(chan os.Signal)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	// capture the signal
	select {
	case <-sigs:
	}
	// reject all request to public server
	pubServerEnable = false
	if err := clients.DeactivateAll(); err != nil {
		log.Warn("errors occur when deactivating game servers: %v", err)
	}
	// waiting for all game server unregister
	for clients.NumOfGS() > 0 {
	}
	allGSReleased = true
	// waiting for all functions in function queue to be done
	for !progCanExit {
	}

	log.Info("the auth server is gracefully exit...")
	time.Sleep(1 * time.Second)
	os.Exit(1)
}
