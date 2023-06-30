package ipfs

import (
	"context"
	"os"
	"sync"

	"berty.tech/go-orbit-db/iface"
	files "github.com/ipfs/go-ipfs-files"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// Given a path get the corresponding node where node is a common interface for
// files, directories and other special files
func GetIPFSNode(path string) (files.Node, error) {
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
func ConnectToPeers(ctx context.Context, orbitdb *iface.OrbitDB, peers []string) error {
	var wg sync.WaitGroup

	api := (*orbitdb).IPFS()

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
			}
		}(peerInfo)
	}
	wg.Wait()
	return nil
}
