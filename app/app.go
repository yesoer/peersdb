package app

import (
	"peersdb/config"
	"sync"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/iface"
	"github.com/ipfs/kubo/core"
)

// TODO : should have a peers store to permanently add/remove known peers which
// we will try to connect to on startup
//
// represents the application across go routines
type PeersDB struct {
	// data storage
	Node          *core.IpfsNode         // TODO : only because of node.PeerHost.EventBus
	Contributions *orbitdb.EventLogStore // the log which holds all contributions
	Validations   *orbitdb.DocumentStore // the store which holds all validations
	Orbit         *iface.OrbitDB

	// mutex to control access to the eventlog db across go routines
	ContributionsMtx sync.RWMutex
	ValidationsMtx   sync.RWMutex

	// persisted peersdb config
	Config *config.Config

	// benchmarks
	Benchmark *Benchmark
}

// TODO : check out orbitdb logger (apparently safe for concurrent use and lightweight
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
