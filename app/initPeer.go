package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"peersdb/config"
	"peersdb/ipfs"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"berty.tech/go-orbit-db/stores/documentstore"
	"github.com/ipfs/kubo/core/coreapi"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	"go.uber.org/zap"
)

var orbit iface.OrbitDB

// starts the ipfs node and creates the orbitdb structures on top of it
//
// DEVNOTE : PeersDB.EventLogDB may be nil after init ! that is if it's not root
// and has no transaction datastore locally. A datastore will be replicated on
// the first established peer connection
func InitPeer(peersDB *PeersDB, bench *Benchmark) error {

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
		EventBus:         eventbus.NewBus(),
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
				(*peersDB.Orbit).Identity().ID,
			},
			"read": {
				(*peersDB.Orbit).Identity().ID,
			},
		},
	}

	storeType = "docstore"
	create := true
	replicate := false // no one else has write access
	docstoreOpt := documentstore.DefaultStoreOptsForMap("path")
	validationsCache := filepath.Join(cache, "validations")
	dbopts = orbitdb.CreateDBOptions{
		Create:            &create,
		StoreType:         &storeType,
		StoreSpecificOpts: docstoreOpt,
		AccessController:  ac,
		Directory:         &validationsCache,
		Replicate:         &replicate,
		EventBus:          eventbus.NewBus(),
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
	if bench != nil {
		peersDB.Benchmark = bench
	} else {
		peersDB.Benchmark = &Benchmark{}
	}

	if *config.FlagRegion != "" {
		peersDB.Benchmark.Region = *config.FlagRegion
	}

	// connect to a bootstrap peer
	if *config.FlagBootstrap != "" {
		fmt.Print("\nbootstrap : ", *config.FlagBootstrap, "\n")
		IssueConnectCmd(peersDB, []string{*config.FlagBootstrap})
	}

	return nil
}

// connect to a peer given their IP, by sending an http request for the "CONNECT"
// cmd, with own connection string
func IssueConnectCmd(peersDB *PeersDB, peers []string) {
	client := &http.Client{}

	// for each given ip, send the connect request
	for _, p := range peers {
		// TODO : can we get the address from the coreapi ?
		myIP := getOwnIP()
		if p == "127.0.0.1" {
			myIP = p
		}
		myAddr := "/ip4/" + myIP + "/tcp/" + *config.FlagIPFSPort + "/p2p/" + peersDB.Config.PeerID
		fmt.Print("\n sending my address : ", myAddr, " to IP ", p, "\n")

		// TODO : port may be different aswell
		cmdPath := "http://" + p + ":8080/peersdb/command"

		connectReq := Request{CONNECT, []string{myAddr}}
		jsonData, err := json.Marshal(connectReq)
		if err != nil {
			fmt.Println("Error marshaling JSON:", err)
			return
		}

		req, err := http.NewRequest("POST", cmdPath, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Println("Error creating request:", err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("Error sending request:", err)
			continue
		}
		defer resp.Body.Close()
	}
}

func getOwnIP() string {
	// get the list of network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("Failed to get network interfaces:", err)
		return ""
	}

	// iterate through each network interface
	for _, iface := range interfaces {
		// check if the interface is up and not a loopback
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			// get the addresses for the current interface
			addrs, err := iface.Addrs()
			if err != nil {
				fmt.Println("Failed to get addresses for interface", iface.Name, ":", err)
				continue
			}

			// iterate through each address
			for _, addr := range addrs {
				// check if the address is an IP address
				ipNet, ok := addr.(*net.IPNet)
				if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
					// print the IP address
					return ipNet.IP.String()
				}
			}
		}
	}

	return ""
}
