package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"peersdb/config"
	"peersdb/ipfs"
	"strings"
	"time"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"berty.tech/go-orbit-db/stores"
	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/interface-go-ipfs-core/path"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"golang.org/x/net/context"
)

// Method defines simple schemas for actions to perform on the db
type Method struct {
	Cmd    string `json:"cmd"`
	ArgCnt int    `json:"argcnt"`
}

var (
	GET     Method = Method{"get", 1}     // needs the filepath
	POST    Method = Method{"post", 1}    // needs the cid
	CONNECT Method = Method{"connect", 1} // needs the peer iden
	QUERY   Method = Method{"query", 0}
)

// Requests are an abstraction for the communication between this applications
// various apis (shell, http, grpc etc.) and the actual db service
// (n to 1 relation at the moment)
type Request struct {
	Method Method   `json:"method"`
	Args   []string `json:"args"`
}

func Service(peersDB *PeersDB,
	reqChan chan Request,
	resChan chan interface{},
	logChan chan Log) {

	// wait for and handle connectedness changed event
	go awaitConnected(peersDB, logChan)

	// wait for pubsub messages to self which will be received when another peer
	// connects (see "awaitConnected" above)
	go awaitStoreExchange(peersDB, logChan)

	// wait for write events to handle validation
	go awaitWriteEvent(peersDB, logChan)

	//--------------------------------------------------------------------------
	// handle API requests

	// TODO : for http api it would be nice to return errors instead of
	// logging them
	for {
		req := <-reqChan
		var res interface{}

		switch req.Method.Cmd {
		case GET.Cmd:
			res = "Not implemented"
		case POST.Cmd:
			path := req.Args[0]
			res = post(peersDB, path, logChan)

		case CONNECT.Cmd:
			peerId := req.Args[0]
			res = connect(peersDB, peerId, logChan)

		case QUERY.Cmd:
			res = query(peersDB, logChan)
		}

		// send response
		resChan <- res
	}
}

// waits for connectedness changed events and on success sends the stores id
func awaitConnected(peersDB *PeersDB, logChan chan Log) {
	// subscribe to ipfs level connectedness changed event
	subipfs, err := (*peersDB.Node).PeerHost.EventBus().Subscribe(
		new(event.EvtPeerConnectednessChanged))
	if err != nil {
		logChan <- Log{Type: NonRecoverableErr, Data: err}
		return
	}

	db := peersDB.Contributions
	coreAPI := (*peersDB.Orbit).IPFS()

	for e := range subipfs.Out() {
		e, ok := e.(event.EvtPeerConnectednessChanged)

		// on established connection
		if ok && e.Connectedness == network.Connected && db != nil {
			// send this stores id to peer by publishing it to the topic
			// identified by their id
			peerId := e.Peer.String()
			cidDbId := (*db).Address().String()
			ctx := context.Background()
			err := coreAPI.PubSub().Publish(ctx, peerId, []byte(cidDbId))
			if err != nil {
				logChan <- Log{Type: RecoverableErr, Data: err}
			}

			continue
		}
	}
}

// on connectedness changed events, peers exchange their event logs
func awaitStoreExchange(peersDB *PeersDB, logChan chan Log) {
	// subscribe to own topic
	nodeId := peersDB.Config.PeerID
	coreAPI := (*peersDB.Orbit).IPFS()
	ctx := context.Background()
	sub, err := coreAPI.PubSub().Subscribe(ctx, nodeId)
	if err != nil {
		logChan <- Log{Type: NonRecoverableErr, Data: err}
		return
	}

	for {
		// received data should contain the id of the peers db
		msg, err := sub.Next(context.Background())
		if err != nil {
			logChan <- Log{Type: NonRecoverableErr, Data: err}
			return
		}

		// in case we started without any db, replicate this one
		if peersDB.Contributions == nil {
			addr := string(msg.Data())
			create := false
			storeType := "eventlog"

			// give anyone write access
			ac := &accesscontroller.CreateAccessControllerOptions{
				Access: map[string][]string{
					"write": {
						"*",
					},
				},
			}

			dbopts := orbitdb.CreateDBOptions{
				AccessController: ac,
				Create:           &create,
				StoreType:        &storeType,
			}

			store, err := (*peersDB.Orbit).Open(ctx, addr, &dbopts)
			if err != nil {
				logChan <- Log{Type: RecoverableErr, Data: err}
			}

			db := store.(iface.EventLogStore)
			db.Load(ctx, -1)
			peersDB.Contributions = &db

			// persist store address
			peersDB.Config.ContributionsStoreAddr = addr
			config.SaveConfig(peersDB.Config)
		}
	}
}

type opDoc struct {
	Key   string `json:"key,omitempty"`
	Value []byte `json:"value,omitempty"`
}

func extractCIDFromIPFSPath(ipfsPath string) (string, error) {
	// Remove the "/ipfs/" prefix from the IPFS path
	cidStartIndex := strings.Index(ipfsPath, "/ipfs/") + 6
	if cidStartIndex < 6 || cidStartIndex >= len(ipfsPath) {
		return "", fmt.Errorf("invalid IPFS path")
	}

	cid := ipfsPath[cidStartIndex:]
	return cid, nil
}

func awaitWriteEvent(peersDB *PeersDB, logChan chan Log) {
	// since contributions datastore may be nil, wait till it isn't
	for peersDB.Contributions == nil {
		time.Sleep(time.Second)
	}

	// subscribe to write event
	contributions := *peersDB.Contributions
	subdb, err := contributions.EventBus().Subscribe([]interface{}{
		new(stores.EventWrite),
	})
	if err != nil {
		logChan <- Log{RecoverableErr, err}
		return
	}

	coreAPI := (*peersDB.Orbit).IPFS()
	ctx := context.Background()

	validations := *peersDB.Validations

	subChan := subdb.Out()
	for {
		// get the new entry
		e := <-subChan
		we := e.(stores.EventWrite)

		// check if the write was executed on the contributions db
		if we.Address.GetPath() != contributions.Address().GetPath() {
			continue
		}

		entry := we.Entry

		// get the ipfs-log operation from the entry
		opStr := entry.GetPayload()
		var op opDoc
		err := json.Unmarshal(opStr, &op)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		// get the ipfs file path the from the contribution block
		var contribution Contribution
		err = json.Unmarshal(op.Value, &contribution)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}
		pth := contribution.Path

		// get the file from ipfs
		parsedPth := path.New(pth)
		file, err := coreAPI.Unixfs().Get(ctx, parsedPth)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		// try to validate the file
		valid, err := validateStub(file)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		// store validation info
		valdoc := map[string]interface{}{
			"Path":    pth,
			"IsValid": valid,
		}
		_, err = validations.Put(ctx, valdoc)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}
	}
}

// TODO : implement validation
func validateStub(file files.Node) (bool, error) {
	return true, nil
}

type Contribution struct {
	Path        string `json:"path"`        // ipfs file path which includes the cid
	Contributor string `json:"contributor"` // ipfs node id
}

// executes post command
func post(peersDB *PeersDB, path string, logChan chan Log) interface{} {
	db := peersDB.Contributions
	if db == nil {
		err := errors.New("you need a datastore first, try connecting to a peer")
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}

	// add a file to the ipfs store
	// DEVNOTE : test file : "./sample-data/r4.xlarge_i-0f4e4b248a6aa957a_wordcount_spark_large_3/sar.csv"
	ctx := context.Background()
	coreAPI := (*peersDB.Orbit).IPFS()
	filePath, err := ipfs.AddToIPFSByPath(ctx, coreAPI, path)
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}

	// create the contribution block
	ipfsPath := filePath.String()
	data := Contribution{ipfsPath, peersDB.Config.PeerID}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}

	// add the contribution block
	_, err = (*db).Add(ctx, dataJSON)
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}

	return "File uploaded"
}

// executes connect command
func connect(peersDB *PeersDB, peerId string, logChan chan Log) string {
	ctx := context.Background()
	err := ipfs.ConnectToPeers(ctx, peersDB.Orbit, []string{peerId})
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
	}
	return "Peer id processed"
}

// executes query command
func query(peersDB *PeersDB, logChan chan Log) []Contribution {
	db := peersDB.Contributions
	if db == nil {
		err := errors.New("you need a datastore first, try connecting to a peer")
		logChan <- Log{Type: RecoverableErr, Data: err}
		return nil
	}

	// fetch data from network
	infinity := -1
	ctx := context.Background()
	(*db).Load(ctx, infinity)

	// TODO : await replication/ready event
	time.Sleep(time.Second * 5)

	// get all entries and parse them
	res, err := (*db).List(ctx, &orbitdb.StreamOptions{Amount: &infinity})
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
	}

	jsonRes := make([]Contribution, len(res))
	for i, op := range res {
		err := json.Unmarshal(op.GetValue(), &jsonRes[i])
		if err != nil {
			logChan <- Log{Type: RecoverableErr, Data: err}
		}
	}

	return jsonRes
}
