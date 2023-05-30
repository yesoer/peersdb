package main

import (
	"errors"
	"fmt"
	"time"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"golang.org/x/net/context"
)

func service(peersDB *PeersDB, reqChan chan Request, resChan chan interface{}, logChan chan Log) {
	coreAPI := (*peersDB.Orbit).IPFS()

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
		db := peersDB.EventLogDB

		for e := range subipfs.Out() {
			e, ok := e.(event.EvtPeerConnectednessChanged)

			// on established connection
			if ok && e.Connectedness == network.Connected && db != nil {
				// send this stores id to peer by publishing it to the topic
				// identified by their id
				peerId := e.Peer.String()
				cidDbId := (*db).Address().String()
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
					logChan <- Log{RecoverableErr, err}
				}

				db := store.(iface.EventLogStore)
				db.Load(ctx, -1)
				peersDB.EventLogDB = &db

				// persist store address
				peersDB.Config.StoreAddr = addr
				SaveConfig(peersDB.Config)
			}
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
			db := peersDB.EventLogDB
			if db == nil {
				err = errors.New("you need a datastore first, try connecting to a peer")
				logChan <- Log{RecoverableErr, err}
				break
			}

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
			_, err = (*db).Add(ctx, data)
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
			db := peersDB.EventLogDB
			if db == nil {
				err = errors.New("you need a datastore first, try connecting to a peer")
				logChan <- Log{RecoverableErr, err}
				break
			}

			infinity := -1
			(*db).Load(ctx, infinity)
			// TODO : await replication/ready event
			time.Sleep(time.Second * 5)
			res, err := (*db).List(ctx, &orbitdb.StreamOptions{Amount: &infinity})
			if err != nil {
				logChan <- Log{RecoverableErr, err}
			}
			fmt.Print(res)
		}
	}
}
