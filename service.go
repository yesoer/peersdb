package main

import (
	"errors"
	"time"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/net/context"
)

type PeerDoc struct {
	AddrInfo peer.AddrInfo
}

func service(peersDB *PeersDB, reqChan chan Request, resChan chan interface{}, logChan chan Log) {
	coreAPI := (*peersDB.Orbit).IPFS()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // TODO : correct ?

	//-------------------------------------------------------------------------
	// CONNECT TO KNOWN PEERS
	//-------------------------------------------------------------------------

	// get all peers address infos
	peers, err := (*peersDB.PeerAddrDB).Query(ctx, func(doc interface{}) (bool, error) {
		return true, nil
	})
	if err != nil {
		logChan <- Log{NonRecoverableErr, err}
	}

	peerMAs := make([][]multiaddr.Multiaddr, len(peers))
	for i, peer := range peers {
		p := peer.(PeerDoc)
		peerMAs[i] = p.AddrInfo.Addrs
	}

	// try connecting to known peers
	// TODO : solve channel issue
	dummyChan := make(chan Log, 1)
	ConnectToPeers(ctx, peersDB, peerMAs, dummyChan)

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
		db := peersDB.TransactionsDB

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
			msg, err := sub.Next(context.Background())
			if err != nil {
				logChan <- Log{NonRecoverableErr, err}
				return
			}

			// store the peers addresses
			peerid := msg.From()
			addrInfo, err := (*peersDB.Orbit).IPFS().Dht().FindPeer(ctx, peerid)
			if err != nil {
				logChan <- Log{RecoverableErr, err}
			}

			peerdoc := PeerDoc{
				AddrInfo: addrInfo,
			}
			(*peersDB.PeerAddrDB).Put(ctx, peerdoc)

			// in case we started without any db, replicate this one
			if peersDB.TransactionsDB == nil {
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
				peersDB.TransactionsDB = &db

				// persist store address
				peersDB.Config.TransactionsStoreAddr = addr
				SaveConfig(peersDB.Config)
			}
		}
	}()

	//--------------------------------------------------------------------------
	// handle API requests
	// TODO : send results back to api via respond channel
	for {
		req := <-reqChan
		var res interface{}

		switch req.Method.Cmd {
		case GET.Cmd:
			res = "Not implemented"
		case POST.Cmd:
			db := peersDB.TransactionsDB
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

			res = "File uploaded"

		case CONNECT.Cmd:
			peerAddr, err := multiaddr.NewMultiaddr(req.Args[0])
			if err != nil {
				logChan <- Log{RecoverableErr, err}
			}

			peers := [][]multiaddr.Multiaddr{{peerAddr}}
			ConnectToPeers(ctx, peersDB, peers, logChan)
			res = "Peer address processed"

		// TODO : only works once ?!
		case QUERY.Cmd:
			db := peersDB.TransactionsDB
			if db == nil {
				err = errors.New("you need a datastore first, try connecting to a peer")
				logChan <- Log{RecoverableErr, err}
				break
			}

			infinity := -1
			(*db).Load(ctx, infinity)
			// TODO : await replication/ready event
			time.Sleep(time.Second * 5)
			res, err = (*db).List(ctx, &orbitdb.StreamOptions{Amount: &infinity})
			if err != nil {
				logChan <- Log{RecoverableErr, err}
			}
		}

		// send response
		resChan <- res
	}
}
