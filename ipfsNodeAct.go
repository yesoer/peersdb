package main

import (
	"context"
	"os"
	"peersdb/app"
	"sync"

	files "github.com/ipfs/go-ipfs-files"
	icore "github.com/ipfs/interface-go-ipfs-core"
	"github.com/ipfs/interface-go-ipfs-core/path"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// Adds a file to orbitdb by adding it to ipfs and storing the resulting hash
// in orbitdb
func AddToIPFSByPath(ctx context.Context, IPFSCoreAPI icore.CoreAPI, path string) (path.Resolved, error) {
	// get a file as node
	node, err := getIPFSNode(path)
	if err != nil {
		return nil, err
	}

	// store node in ipfs' blockstore as merkleDag and get it's key (= path)
	key, err := IPFSCoreAPI.Unixfs().Add(ctx, node)
	if err != nil {
		return nil, err
	}

	return key, err
}

// Given a path get the corresponding node where node is a common interface for
// files, directories and other special files
func getIPFSNode(path string) (files.Node, error) {
	fileStat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	node, err := files.NewSerialFile(path, true, fileStat)
	if err != nil {
		return nil, err
	}

	return node, nil
}

var loadPluginsOnce sync.Once

// try to connect to the given peers
func ConnectToPeers(ctx context.Context, peersDB *app.PeersDB, peers []string, logChan chan app.Log) error {
	var wg sync.WaitGroup

	api := (*peersDB.Orbit).IPFS()

	// extract and map ids to addresses
	peerInfos := make(map[peer.ID]*peer.AddrInfo, len(peers))
	for _, addrStr := range peers {
		addr, err := ma.NewMultiaddr(addrStr)
		if err != nil {
			return err
		}
		pii, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			return err
		}
		pi, ok := peerInfos[pii.ID]
		if !ok {
			pi = &peer.AddrInfo{ID: pii.ID}
			peerInfos[pi.ID] = pi
		}
		pi.Addrs = append(pi.Addrs, pii.Addrs...)
	}

	// try to connect to peers
	wg.Add(len(peerInfos))
	for _, peerInfo := range peerInfos {
		go func(peerInfo *peer.AddrInfo) {
			defer wg.Done()
			err := api.Swarm().Connect(ctx, *peerInfo)
			if err != nil {
				// TODO : should be sent via Response channel
				logChan <- app.Log{Type: app.RecoverableErr, Data: err}
			}
		}(peerInfo)
	}
	wg.Wait()
	return nil
}
