package app

import (
	"errors"
	"peersdb/config"
	"peersdb/ipfs"
	"time"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"berty.tech/go-orbit-db/stores/operation"
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

	// wait and handle connectedness changed event
	go awaitConnected(peersDB, logChan)

	// wait for pubsub messages to self which will be received when another peer
	// connects (see "awaitConnected" above)
	go awaitStoreExchange(peersDB, logChan)

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

	db := peersDB.EventLogDB
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
	nodeId := peersDB.ID
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
		if peersDB.EventLogDB == nil {
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
			peersDB.EventLogDB = &db

			// persist store address
			peersDB.Config.StoreAddr = addr
			config.SaveConfig(peersDB.Config)
		}
	}
}

// executes post command
func post(peersDB *PeersDB, path string, logChan chan Log) interface{} {
	db := peersDB.EventLogDB
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

	// add the reference to the file (stored in ipfs) to orbitdb
	data := []byte(filePath.String())
	_, err = (*db).Add(ctx, data)
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
func query(peersDB *PeersDB, logChan chan Log) []operation.Operation {
	db := peersDB.EventLogDB
	if db == nil {
		err := errors.New("you need a datastore first, try connecting to a peer")
		logChan <- Log{Type: RecoverableErr, Data: err}
		return nil
	}

	infinity := -1
	ctx := context.Background()
	(*db).Load(ctx, infinity)

	// TODO : await replication/ready event
	time.Sleep(time.Second * 5)

	res, err := (*db).List(ctx, &orbitdb.StreamOptions{Amount: &infinity})
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
	}

	return res
}
