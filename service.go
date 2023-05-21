package main

import (
	"fmt"
	"sync"
	"time"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/iface"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"golang.org/x/net/context"
)

// TODO : should probably be a part of the PeersDB struct
var PeerDbIds []string

func service(peersDB *PeersDB, reqChan chan Request, resChan chan interface{}, logChan chan Log) {
	db := *peersDB.EventLogDB
	coreAPI := db.IPFS()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // TODO : correct ?

	//-------------------------------------------------------------------------
	// EVENTS
	//-------------------------------------------------------------------------

	// subscribe to ipfs level connectedness changed event
	subipfs, err := (*peersDB.Node).PeerHost.EventBus().Subscribe(
		new(event.EvtPeerConnectednessChanged))
	if err != nil {
		logChan <- Log{NonRecoverableErr, err}
		return
	}

	// wait and handle connectedness changed event
	go func() {
		for e := range subipfs.Out() {
			e, ok := e.(event.EvtPeerConnectednessChanged)

			// on established connection send this nodes id to peer by
			// publishing it to the topic identified by their id
			if ok && e.Connectedness == network.Connected {
				peerId := e.Peer.String()
				cidDbId := db.Address().String()
				err := coreAPI.PubSub().Publish(ctx, peerId, []byte(cidDbId))
				if err != nil {
					logChan <- Log{RecoverableErr, err}
				}

				continue
			}
		}
	}()

	//-------------------------------------------------------------------------
	// PubSub
	//-------------------------------------------------------------------------

	// subscribe to own topic
	nodeId := peersDB.ID
	sub, err := coreAPI.PubSub().Subscribe(ctx, nodeId)
	if err != nil {
		logChan <- Log{NonRecoverableErr, err}
		return
	}

	// wait for pubsub messages to self which will be received when another peer
	// connects (see "EVENTS" section : "EvtPeerConnectednessChanged")
	go func() {
		for {
			// received data should contain the id of the peers db
			msg, err := sub.Next(context.Background())
			if err != nil {
				logChan <- Log{NonRecoverableErr, err}
				return
			}

			PeerDbIds = append(PeerDbIds, string(msg.Data()))
		}
	}()

	//--------------------------------------------------------------------------
	// handle API requests
	// TODO : send results back to api via respond channel
	for {
		req := <-reqChan
		switch req.Method.Cmd {
		case GET.Cmd:

		case POST.Cmd:
			// add a file to the ipfs store
			// DEVNOTE : test file : "./sample-data/r4.xlarge_i-0f4e4b248a6aa957a_wordcount_spark_large_3/sar.csv"
			path := req.Args[0]
			filePath, err := AddToIPFSByPath(ctx, coreAPI, path)
			if err != nil {
				logChan <- Log{RecoverableErr, err}
				break
			}

			// add the reference to the file (stored in ipfs) to orbitdb
			data := []byte(filePath.String())
			_, err = db.Add(ctx, data)
			if err != nil {
				logChan <- Log{RecoverableErr, err}
			}

		case CONNECT.Cmd:
			peerId := req.Args[0]
			err = ConnectToPeers(ctx, peersDB, []string{peerId}, logChan)
			if err != nil {
				logChan <- Log{RecoverableErr, err}
			}

		// TODO : only works once ?!
		case QUERY.Cmd:
			distQuery(ctx, peersDB, logChan)
		}
	}
}

// execute a given query on known DBs of known peers
func distQuery(ctx context.Context, peersDB *PeersDB, logChan chan Log) {
	allowCreate := false
	storeType := "docstore"
	createOpt := iface.CreateDBOptions{Create: &allowCreate, StoreType: &storeType}

	var wg sync.WaitGroup
	wg.Add(len(PeerDbIds))

	// execute distributed query to get all cids
	fmt.Print("known dbs : ", PeerDbIds)
	for _, dbid := range PeerDbIds {
		go func(dbid string) {

			defer wg.Done()

			// get peers db
			peerDb, err := (*peersDB.Orbit).Open(ctx, dbid, &createOpt)
			if err != nil {
				logChan <- Log{RecoverableErr, err}
				return
			}
			infinity := -1
			peerDb.Load(ctx, infinity) // TODO : fetch all entries

			// TODO : await that db is ready/loaded/replicated ?
			fmt.Print("before ", peerDb.ReplicationStatus())
			time.Sleep(time.Second * 3)
			fmt.Print("after ", peerDb.ReplicationStatus())

			defer peerDb.Drop()
			defer peerDb.Close()

			// try converting to doc store
			// TODO : filter should be passed as arg to query
			ds, _ := peerDb.(iface.EventLogStore)
			defer ds.Close()
			defer ds.Drop()

			res, err := ds.List(ctx, &orbitdb.StreamOptions{Amount: &infinity})
			if err != nil {
				logChan <- Log{RecoverableErr, err}
				return
			}

			fmt.Print(res)
		}(dbid)
	}

	wg.Wait()
}
