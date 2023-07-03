package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"peersdb/config"
	"peersdb/ipfs"
	"strings"
	"time"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/accesscontroller"
	"berty.tech/go-orbit-db/iface"
	"berty.tech/go-orbit-db/stores"
	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/interface-go-ipfs-core/options"
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
	GET       Method = Method{"get", 1}     // needs the ipfs filepath
	POST      Method = Method{"post", 1}    // needs a string of bytes representing the file
	CONNECT   Method = Method{"connect", 1} // needs the peer address
	QUERY     Method = Method{"query", 0}
	BENCHMARK Method = Method{"benchmark", 0}
)

// Requests are an abstraction for the communication between this applications
// various apis (shell, http, grpc etc.) and the actual db service
// (n to 1 relation at the moment)
type Request struct {
	Method Method   `json:"method"`
	Args   []string `json:"args"`
}

// starts all reoccuring tasks on peersdb level
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

	// wait for and handle "validation" requests
	go awaitValidationReq(peersDB, logChan)

	// wait for and handle replication event
	go awaitReplicateEvent(peersDB, logChan)

	//--------------------------------------------------------------------------
	// handle API requests

	// TODO : for http api it would be nice to return errors instead of
	// logging them
	for {
		req := <-reqChan
		var res interface{}

		switch req.Method.Cmd {
		case GET.Cmd:
			ipfsPath := req.Args[0]
			res = get(peersDB, ipfsPath, logChan)

		case POST.Cmd:
			file := req.Args[0]
			node := files.NewBytesFile([]byte(file))
			res = post(peersDB, node, logChan)

		case CONNECT.Cmd:
			// type checking
			peerId := req.Args[0]
			res = connect(peersDB, peerId, logChan)

		case QUERY.Cmd:
			res = query(peersDB, logChan)

		case BENCHMARK.Cmd:
			if !*config.FlagBenchmark {
				res = "Benchmark is not enabled, use -benchmark to do so"
			}

			res = *peersDB.Benchmark
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
	defer subdb.Close()

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
			"path":    pth,
			"isValid": valid,
			"voteCnt": 0,
		}

		logChan <- Log{Info, fmt.Sprintf("validated %s with result %t",
			valdoc["path"], valdoc["isValid"])}

		peersDB.ValidationsMtx.Lock()
		_, err = validations.Put(ctx, valdoc)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}
		peersDB.ValidationsMtx.Unlock()
	}
}

// TODO : implement validation
func validateStub(file files.Node) (bool, error) {
	return true, nil
}

type Contribution struct {
	Path        string    `json:"path"`        // ipfs file path which includes the cid
	Contributor string    `json:"contributor"` // ipfs node id
	CreationTS  time.Time `json:"creationTS"`  // timestamp of creation
}

func get(peersDB *PeersDB, ipfsPath string, logChan chan Log) interface{} {
	db := *peersDB.Contributions
	coreAPI := db.IPFS()
	ctx := context.Background()

	pth := path.New(ipfsPath)
	n, err := coreAPI.Unixfs().Get(ctx, pth)
	if err != nil {
		logChan <- Log{RecoverableErr, err}
		return nil
	}

	// determine destination location
	// TODO : can we get the file info/name from the node ?
	// otherwise add it to contribution block metadata
	fileName := strings.TrimPrefix(ipfsPath, "/ipfs/")
	dest := *config.FlagDownloadDir + fileName
	if dest[:2] == "~/" {
		// expand the tilde (~) notation to the user's home directory
		usr, err := user.Current()
		if err != nil {
			return err
		}
		dir := usr.HomeDir
		dest = filepath.Join(dir, dest[2:])
	}

	if err := files.WriteTo(n, dest); err != nil {
		logChan <- Log{RecoverableErr, err}
		return nil
	}

	return "stored " + ipfsPath + " successfully under " + dest
}

// executes post command
func post(peersDB *PeersDB, node files.Node, logChan chan Log) interface{} {
	ctx := context.Background()
	coreAPI := (*peersDB.Orbit).IPFS()

	// contributions store may be nil for non-root nodes
	db := peersDB.Contributions
	if db == nil {
		err := errors.New("you need a datastore first, try connecting to a peer")
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}

	// store node in ipfs' blockstore as merkleDag and get it's key (= path)
	filePath, err := coreAPI.Unixfs().Add(ctx, node)
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}

	// create the contribution block
	ipfsPath := filePath.String()
	ts := time.Now()
	data := Contribution{ipfsPath, peersDB.Config.PeerID, ts}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}

	// TODO : check if it already exists/the data has been added already

	// add the contribution block
	peersDB.ContributionsMtx.Lock()
	_, err = (*db).Add(ctx, dataJSON)
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
		return err
	}
	peersDB.ContributionsMtx.Unlock()

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

	// TODO : await ready event
	time.Sleep(time.Second * 5)

	// get all entries and parse them
	res, err := (*db).List(ctx, &orbitdb.StreamOptions{Amount: &infinity})
	if err != nil {
		logChan <- Log{Type: RecoverableErr, Data: err}
		return []Contribution{}
	}

	jsonRes := make([]Contribution, len(res))
	for i, op := range res {
		err := json.Unmarshal(op.GetValue(), &jsonRes[i])
		if err != nil {
			logChan <- Log{Type: RecoverableErr, Data: err}
			continue
		}

		// TODO : optionally filter by validity
		valid, err := isValid(peersDB, jsonRes[i].Path)
		if err == nil && valid {
			fmt.Print("valid file found")
		}

		if err != nil {
			logChan <- Log{Type: RecoverableErr, Data: err}
		}
	}

	return jsonRes
}

type Validation struct {
	Path    string `json:"path"` // ipfs path for a file, looks like this : /ipfs/<file cid>
	IsValid bool   `json:"isValid"`
	VoteCnt uint32 `json:"voteCnt"` // how many peers have contributed a vote, 0 if it was self determined
}

// checks if the file identified by the ipfs path is valid according to local
// entries or peers
func isValid(peersDB *PeersDB, path string) (bool, error) {
	// check local entry
	validations := *peersDB.Validations
	getopts := iface.DocumentStoreGetOptions{
		CaseInsensitive: false,
		PartialMatches:  false,
	}
	ctx := context.Background()
	local, err := validations.Get(ctx, path, &getopts)
	if err != nil {
		return false, err
	}

	// found a local entry
	if len(local) >= 1 {
		valdoc := local[0].(map[string]interface{})
		isValid := valdoc["isValid"].(bool)
		return isValid, nil
	}

	// no local entry, so fetch votes via pubsub and accumulate them
	validation, err := accValidations(peersDB, path)
	if err != nil {
		return false, err
	}

	// persist result
	valdoc := map[string]interface{}{
		"path":    validation.Path,
		"isValid": validation.IsValid,
		"voteCnt": validation.VoteCnt,
	}

	// TODO : not 100% sure we need these locks
	peersDB.ValidationsMtx.Lock()
	_, err = validations.Put(ctx, valdoc)
	if err != nil {
		isValid := valdoc["isValid"].(bool)
		return isValid, err
	}
	peersDB.ValidationsMtx.Unlock()

	return validation.IsValid, nil
}

type ValidationReq struct {
	Path   string `json:"path"`
	PeerID string `json:"peerId"`
}
type ValidationRes struct {
	Vote bool `json:"vote"`
}

const validationReqTopic = "validation"

// requests and accumulates votes via pubsub
// returns a probability between 0 and 1 for validity of data
func accValidations(peersDB *PeersDB, pth string) (Validation, error) {
	// receive votes via topic : this nodes id + the files path
	coreAPI := (*peersDB.Orbit).IPFS()
	nodeId := (*peersDB.Config).PeerID
	ctx := context.Background()
	resSub, err := coreAPI.PubSub().Subscribe(ctx, nodeId+pth)
	if err != nil {
		return Validation{}, err
	}

	// announce their wish via topic : "validation" with message data : their id + the files cid
	req := ValidationReq{pth, nodeId}
	reqData, err := json.Marshal(req)
	if err != nil {
		return Validation{}, err
	}

	err = coreAPI.PubSub().Publish(ctx, validationReqTopic, reqData)
	if err != nil {
		return Validation{}, err
	}

	// wait 5 seconds to accumulate votes
	timeout := 5 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	validCnt := 0
	inValidCnt := 0
	ret := false
	for {
		select {
		case <-ctx.Done():
			// the deadline has been reached or the context was canceled
			ret = true
		default:
			msg, err := resSub.Next(ctx)
			if err != nil {
				// TODO : log error
				continue
			}

			// accumulate votes
			var res ValidationRes
			err = json.Unmarshal(msg.Data(), &res)
			if err != nil {
				// TODO : log error
				continue
			}

			if res.Vote {
				validCnt++
				continue
			}

			inValidCnt++
		}

		if ret {
			break
		}
	}

	totalVotes := validCnt + inValidCnt
	validation := Validation{pth, false, uint32(totalVotes)}

	// if more than half have voted for valid, the data is considered valid
	// else self-validate
	// TODO : use KnownAddrs as reference instead of Peers ?
	peers, err := coreAPI.Swarm().Peers(ctx)
	if err != nil {
		return Validation{}, err
	}
	numPeers := float64(len(peers))
	if float64(validCnt) > (.5 * numPeers) {
		validation.IsValid = true
	} else {

		// get the file from ipfs
		parsedPth := path.New(pth)
		file, err := coreAPI.Unixfs().Get(ctx, parsedPth)
		if err != nil {
			return Validation{}, err
		}

		validation.IsValid, err = validateStub(file)
		if err != nil {
			return Validation{}, err
		}
	}

	return validation, nil
}

// waits for validation requests
func awaitValidationReq(peersDB *PeersDB, logChan chan Log) {
	// receive validation requests via pubsub
	coreAPI := (*peersDB.Orbit).IPFS()
	ctx := context.Background()
	resSub, err := coreAPI.PubSub().Subscribe(ctx, validationReqTopic)
	if err != nil {
		// TODO : is it correct to flag this as recoverable ?
		logChan <- Log{RecoverableErr, err}
	}

	for {
		msg, err := resSub.Next(ctx)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		var validationReq ValidationReq
		err = json.Unmarshal(msg.Data(), &validationReq)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		// from the validation store get the corresponding entry, if any
		validations := *peersDB.Validations
		res, err := validations.Get(ctx, validationReq.Path, &iface.DocumentStoreGetOptions{})
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		// no internal vote
		// TODO : should be reason to listen to the voting topic aswell right ?
		if len(res) < 1 {
			continue
		}

		// only respond if the vote comes from self
		valdoc := res[0].(map[string]interface{})
		e := validationMapToStruct(valdoc)
		if e.VoteCnt != 0 {
			continue
		}

		validationRes := ValidationRes{e.IsValid}
		resTopic := validationReq.PeerID + validationReq.Path
		resData, err := json.Marshal(validationRes)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		err = coreAPI.PubSub().Publish(ctx, resTopic, resData)
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}
	}
}

// creates a validation struct from a map as returned from the validations
// docstore
func validationMapToStruct(m map[string]interface{}) Validation {
	pth := m["path"].(string)
	isValid := m["isValid"].(bool)
	voteCntF := m["voteCnt"].(float64)
	voteCntU := uint32(voteCntF)

	return Validation{
		Path:    pth,
		IsValid: isValid,
		VoteCnt: voteCntU,
	}
}

// wait for the replicated event and pin data if full replication is enabled
// TODO : this is very similar to awaitWriteEvent, try to combine the two and see
// if it makes sense
func awaitReplicateEvent(peersDB *PeersDB, logChan chan Log) {
	// since contributions datastore may be nil, wait till it isn't
	for peersDB.Contributions == nil {
		time.Sleep(time.Second)
	}

	// subscribe to replicated event
	contributions := *peersDB.Contributions
	subdb, err := contributions.EventBus().Subscribe([]interface{}{
		new(stores.EventReplicated),
	})
	if err != nil {
		logChan <- Log{RecoverableErr, err}
		return
	}
	defer subdb.Close()

	coreAPI := (*peersDB.Orbit).IPFS()

	subChan := subdb.Out()
	for {
		// get the new entry
		e := <-subChan
		re := e.(stores.EventReplicated)

		// check if the replication was executed on the contributions db
		if re.Address.GetPath() != contributions.Address().GetPath() {
			continue
		}

		entries := re.Entries

		for _, entry := range entries {
			// get the ipfs-log operation from the entry
			opStr := entry.GetPayload()
			var op opDoc
			err := json.Unmarshal(opStr, &op)
			if err != nil {
				logChan <- Log{RecoverableErr, err}
				continue
			}

			// parse to contribution block
			var contribution Contribution
			err = json.Unmarshal(op.Value, &contribution)
			if err != nil {
				logChan <- Log{RecoverableErr, err}
				continue
			}

			// store bootstrap and new contribution benchmark
			if *config.FlagBenchmark {
				peersDB.Benchmark.UpdateBootstrap(contribution.CreationTS)
				peersDB.Benchmark.UpdateNewContributions(contribution.CreationTS)
			}

			// replicate by adding pin
			if *config.FlagFullReplica {
				pth := contribution.Path

				ctx := context.Background()
				parsedPth := path.New(pth)
				opts := options.Pin.Recursive(true)
				coreAPI.Pin().Add(ctx, parsedPth, opts)
			}
		}
	}
}
