package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/iface"
	"github.com/ipfs/kubo/core"
)

// represents the application across go routines
type PeersDB struct {
	ID string // node identifier TODO : can probably get it from LogDB somehow

	// data storage
	Node  *core.IpfsNode         // TODO : only because of node.PeerHost.EventBus
	LogDB *orbitdb.DocumentStore // the log which holds all transactions
	Orbit *iface.OrbitDB

	// mutex to control access to the eventlog db across go routines
	LogDBMtx sync.RWMutex
}

type LogType uint8

const (
	RecoverableErr    LogType = 0
	NonRecoverableErr LogType = 1
	Info              LogType = 2
	Print             LogType = 3
)

type Log struct {
	Type LogType
	Data interface{}
}

// main function which terminates when SIGINT or SIGTERM is received
// via termCtx/termCancel, any cancellation can be forwarded to go routines for
// graceful shutdown
func main() {

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
	var peersDB PeersDB
	err := initPeer(&peersDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error on setup:\n %+v\n", err)
		os.Exit(1)
	}

	// channels to communicate requests from all apis to the service routine
	// for processing
	// TODO : should be able to hold configurable many requests as buffer
	// TODO : right now only one api can work and it cannot process requests in
	//		  parallel
	reqChan := make(chan Request, 100)
	resChan := make(chan interface{}, 100)

	// channel for centralized loggin
	logChan := make(chan Log, 100)

	// handle logging channel
	go func() {
		for {
			l := <-logChan
			switch l.Type {
			case RecoverableErr:
				err := l.Data.(error)
				fmt.Fprintf(os.Stderr, "Recovering from : %v\n", err)
			case NonRecoverableErr:
				err := l.Data.(error)
				fmt.Fprintf(os.Stderr, "Cannot recover from : %+v\n", err)
				termCancel()
			case Info:
				toLog := l.Data.(string)
				log.Printf("%s\n", toLog)
			case Print:
				fmt.Print(l.Data)
			}
		}
	}()

	// handle the peerdbs lifecycle after start and internal interface for
	// shell/api requests
	go service(&peersDB, reqChan, resChan, logChan)

	// start the shell interface
	go shell(&peersDB, reqChan, resChan, logChan)

	// await termination context
	<-termCtx.Done()
	fmt.Printf("Shutdown")
	// DEVNOTE : general graceful shutdown stuff may go here
}
