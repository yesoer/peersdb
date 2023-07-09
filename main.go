package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"peersdb/api"
	"peersdb/app"
	"peersdb/config"
	"syscall"
)

// main function which terminates when SIGINT or SIGTERM is received
// via termCtx/termCancel, any cancellation can be forwarded to go routines for
// graceful shutdown
func main() {

	flag.Parse()

	// prep termination context
	termCtx, termCancel := context.WithCancel(context.Background())

	// prep channel for os signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// handle os signals
	go func() {
		// wait for receiving an interrupt/termination signal
		<-sigs
		termCancel()
	}()

	// init application
	// TODO : this needs centralized error handling aswell but first check out logging via orbitdb
	var peersDB app.PeersDB
	err := app.InitPeer(&peersDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error on setup:\n %+v\n", err)
		os.Exit(1)
	}

	// channels to communicate requests from all apis to the service routine
	// for processing
	// TODO : should be able to hold configurable many requests as buffer
	// TODO : right now only one api can work and it cannot process requests in
	//		  parallel
	reqChan := make(chan app.Request, 100)
	resChan := make(chan interface{}, 100)

	// channel for centralized loggin
	logChan := make(chan app.Log, 100)

	// handle logging channel
	go func() {
		for {
			l := <-logChan
			switch l.Type {
			case app.RecoverableErr:
				err := l.Data.(error)
				fmt.Fprintf(os.Stderr, "Recovering from : %v\n", err)
			case app.NonRecoverableErr:
				err := l.Data.(error)
				fmt.Fprintf(os.Stderr, "Cannot recover from : %+v\n", err)
				termCancel()
			case app.Info:
				toLog := l.Data.(string)
				log.Printf("%s\n", toLog)
			case app.Print:
				fmt.Print(l.Data)
			}
		}
	}()

	// handle the peerdbs lifecycle after start and internal interface for
	// shell/api requests
	go app.Service(&peersDB, reqChan, resChan, logChan)

	// start the shell interface
	if *config.FlagShell {
		go api.Shell(reqChan, resChan, logChan)
	}

	// start the http interface
	if *config.FlagHTTP {
		go api.ServeHTTP(reqChan, resChan, logChan)
	}

	// await termination context
	<-termCtx.Done()
	fmt.Printf("Shutdown")

	// DEVNOTE : general graceful shutdown stuff may go here

	// write config and benchmark to persistent files
	config.SaveStructAsJSON(peersDB.Config, *config.FlagRepo+"_config")

	benchmarkPath := *config.FlagRepo + "_benchmark"
	config.SaveStructAsJSON(peersDB.Benchmark, benchmarkPath)

	// close orbitdb instance
	(*peersDB.Orbit).Close()
}
