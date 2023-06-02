package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"github.com/ipfs/kubo/core/coreapi"
	"go.uber.org/zap"
)

var flagDevLogs = flag.Bool("devlogs", false, "enable development level logging")
var flagRoot = flag.Bool("root", false, "creating a root node means creating a new datastore")

// starts the ipfs node and creates the orbitdb structures on top of it
//
// DEVNOTE : PeersDB.EventLogDB may be nil after init ! that is if it's not root
// and has no transaction datastore locally. A datastore will be replicated on
// the first established peer connection
func initPeer(peersDB *PeersDB) error {

	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't load config : %+v\n", err)
	}
	peersDB.Config = config

	// start ipfs node
	ctx := context.Background()

	node, err := SpawnEphemeral(ctx)
	if err != nil {
		return err
	}
	peersDB.Node = node
	peersDB.ID = node.Identity.String()

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

	// create orbitdb instance
	cache := GetCachePath()
	orbit, err := orbitdb.NewOrbitDB(
		ctx,
		coreAPI,
		&orbitdb.NewOrbitDBOptions{
			Logger:    devLog,
			Directory: &cache,
		})
	if err != nil {
		return err
	}

	peersDB.Orbit = &orbit

	// give write access to all
	ac := &accesscontroller.CreateAccessControllerOptions{
		Access: map[string][]string{
			"write": {
				"*",
			},
		},
	}

	// create or open transactions store
	// enable transactions store creation if this is a root node
	storeType := "eventlog"
	transactionsCache := filepath.Join(cache, "transactions")
	dbopts := orbitdb.CreateDBOptions{
		Create:           flagRoot,
		StoreType:        &storeType,
		AccessController: ac,
		Directory:        &transactionsCache,
	}

	store, err := orbit.Open(ctx, peersDB.Config.TransactionsStoreAddr, &dbopts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\nTry resolving it by connecting to a peer\n", err)
	} else {
		db := store.(iface.EventLogStore)
		db.Load(ctx, -1)
		peersDB.TransactionsDB = &db

		// persist own transactions store address
		peersDB.Config.TransactionsStoreAddr = db.Address().String()
		SaveConfig(peersDB.Config)
	}

	// create or open peer addr store, which will only be writable
	// by this instance
	storeType = "docstore"
	peerAddrCache := filepath.Join(cache, "peeraddr")
	create := true
	dbopts = orbitdb.CreateDBOptions{
		Create:    &create,
		StoreType: &storeType,
		Directory: &peerAddrCache,
	}

	store, err = (*peersDB.Orbit).Open(ctx, peersDB.Config.PeersStoreAddr, &dbopts)
	if err != nil {
		return err
	}
	db := store.(iface.DocumentStore)
	db.Load(ctx, -1)
	peersDB.PeerAddrDB = &db

	// persist own transactions store address
	peersDB.Config.PeersStoreAddr = db.Address().String()
	SaveConfig(peersDB.Config)

	return nil
}

// returns path where datastores etc. are cached
func GetCachePath() string {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache")
	cache := filepath.Join(cacheDir, "peersdb", *flagRepo)
	return cache
}
