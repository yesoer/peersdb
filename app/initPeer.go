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
	"berty.tech/go-orbit-db/stores/documentstore"
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
	cache := filepath.Join(cacheDir, "peersdb", *config.FlagRepo, "orbitdb")

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
	contributionsCache := filepath.Join(cache, "contributions")
	dbopts := orbitdb.CreateDBOptions{
		Create:           config.FlagRoot,
		StoreType:        &storeType,
		AccessController: ac,
		Directory:        &contributionsCache,
	}

	// see if there is a persisted store available
	store, err := orbit.Open(ctx, conf.ContributionsStoreAddr, &dbopts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\nTry resolving it by connecting to a peer\n", err)
	} else {
		db := store.(iface.EventLogStore)
		db.Load(ctx, -1)
		peersDB.Contributions = &db

		// persist store address
		conf.ContributionsStoreAddr = db.Address().String()
	}

	// a creatable docsstore which no other peer can read or write to
	ac = &accesscontroller.CreateAccessControllerOptions{
		Access: map[string][]string{
			"write": {
				conf.PeerID,
			},
			"read": {
				conf.PeerID,
			},
		},
	}

	storeType = "docstore"
	create := true
	docstoreOpt := documentstore.DefaultStoreOptsForMap("path")
	validationsCache := filepath.Join(cache, "validations")
	dbopts = orbitdb.CreateDBOptions{
		Create:            &create,
		StoreType:         &storeType,
		StoreSpecificOpts: docstoreOpt,
		AccessController:  ac,
		Directory:         &validationsCache,
	}

	// see if there is a persisted store available
	store, err = orbit.Open(ctx, conf.ValidationsStoreAddr, &dbopts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\nTry resolving it by connecting to a peer\n", err)
	} else {
		db := store.(iface.DocumentStore)
		db.Load(ctx, -1)
		peersDB.Validations = &db

		// persist store address
		conf.ValidationsStoreAddr = db.Address().String()
	}

	peersDB.Config = conf
	config.SaveConfig(peersDB.Config)

	return nil
}
