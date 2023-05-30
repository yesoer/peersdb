package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"github.com/ipfs/kubo/core/coreapi"
	"go.uber.org/zap"
)

var orbit iface.OrbitDB

var flagDevLogs = flag.Bool("devlogs", false, "enable development level logging")
var flagRoot = flag.Bool("root", false, "creating a root node means creating a new datastore")

// starts the ipfs node and creates the orbitdb structures on top of it
//
// DEVNOTE : PeersDB.EventLogDB may be nil after init ! that is if it's not root
// and has no transaction datastore locally. A datastore will be replicated on
// the first established peer connection
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

	// create eventlog store or load it from disk and give anyone write access
	storeType := "eventlog"

	ac := &accesscontroller.CreateAccessControllerOptions{
		Access: map[string][]string{
			"write": {
				"*",
			},
		},
	}

	dbopts := orbitdb.CreateDBOptions{
		Create:           flagRoot,
		StoreType:        &storeType,
		AccessController: ac,
	}

	addr := "transactions"
	store, err := orbit.Open(ctx, addr, &dbopts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect to a peer to resolve : %v\n", err)
	} else {
		db := store.(iface.EventLogStore)
		db.Load(ctx, -1)
		peersDB.EventLogDB = &db
	}

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
