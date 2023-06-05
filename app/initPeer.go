package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"peersdb/config"
	"peersdb/ipfs"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"github.com/ipfs/kubo/core/coreapi"
	"go.uber.org/zap"
)

var orbit iface.OrbitDB

// starts the ipfs node and creates the orbitdb structures on top of it
//
// DEVNOTE : PeersDB.EventLogDB may be nil after init ! that is if it's not root
// and has no transaction datastore locally. A datastore will be replicated on
// the first established peer connection
func InitPeer(peersDB *PeersDB) error {

	// start ipfs node
	ctx := context.Background()

	// load persistent config
	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't load config : %+v\n", err)
	}

	// start ipfs node
	node, err := ipfs.SpawnEphemeral(ctx)
	if err != nil {
		return err
	}
	conf.PeerID = node.Identity.String()
	peersDB.Node = node

	coreAPI, err := coreapi.NewCoreAPI(node)
	if err != nil {
		return err
	}

	// switch between noop and dev logger via flag
	var devLog *zap.Logger
	if *config.FlagDevLogs {
		devLog, err = zap.NewDevelopment()
		if err != nil {
			return err
		}
	}

	// set cache dir
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache")
	cache := filepath.Join(cacheDir, "peersdb", *config.FlagRepo, "transactions-store")

	// set orbitdb create options
	orbitopts := &orbitdb.NewOrbitDBOptions{
		Logger:    devLog,
		Directory: &cache,
	}
	if conf.PeerID != "" {
		// TODO : if this does not work store and set Identitiy as non-string
		orbitopts.ID = &conf.PeerID
	}

	// start orbitdb instance
	orbit, err = orbitdb.NewOrbitDB(ctx, coreAPI, orbitopts)
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

	// enable create if this is a root node
	storeType := "eventlog"
	dbopts := orbitdb.CreateDBOptions{
		Create:           config.FlagRoot,
		StoreType:        &storeType,
		AccessController: ac,
	}

	// see if there is a persisted store available
	store, err := orbit.Open(ctx, conf.StoreAddr, &dbopts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\nTry resolving it by connecting to a peer\n", err)
	} else {
		db := store.(iface.EventLogStore)
		db.Load(ctx, -1)
		peersDB.EventLogDB = &db

		// persist store address
		conf.StoreAddr = db.Address().String()
	}

	peersDB.Config = conf
	config.SaveConfig(peersDB.Config)

	return nil
}
