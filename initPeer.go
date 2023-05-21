package main

import (
	"context"
	"flag"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/iface"
	"github.com/ipfs/kubo/core/coreapi"
	"go.uber.org/zap"
)

var orbit iface.OrbitDB

var flagDevLogs = flag.Bool("devlogs", false, "enable development level logging")

// starts the ipfs node and creates the orbitdb structures on top of it
func initPeer(peersDB *PeersDB) error {

	// start ipfs node
	ctx := context.Background()

	var err error

	node, err := SpawnEphemeral(ctx)
	if err != nil {
		return err
	}

	coreAPI, err := coreapi.NewCoreAPI(node)
	if err != nil {
		return err
	}

	// switch between noop and dev logger via flag
	var devLog *zap.Logger
	if *flagDevLogs {
		devLog, err = zap.NewDevelopment()
		if err != nil {
			return err
		}
	}

	// create db
	orbit, err = orbitdb.NewOrbitDB(
		ctx,
		coreAPI,
		&orbitdb.NewOrbitDBOptions{Logger: devLog})
	if err != nil {
		return err
	}

	// create document store, with "hash" as the index
	// TODO : eventstore specific options ?
	var db orbitdb.EventLogStore
	dbopts := orbitdb.CreateDBOptions{}
	db, err = orbit.Log(ctx, "transactions", &dbopts)
	if err != nil {
		return err
	}

	peersDB.EventLogDB = &db
	peersDB.ID = node.Identity.String()
	peersDB.Node = node
	peersDB.Orbit = &orbit

	// cleanup
	// TODO : check if this is enough/even necessary
	// TODO : propagate these to main for graceful shutdown
	// orbit.Close()
	// os.RemoveAll(extIPFSNode.repoPath)
	// cancel()

	return nil
}
