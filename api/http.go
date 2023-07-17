package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"peersdb/app"
	"peersdb/config"
	"regexp"

	"github.com/multiformats/go-multiaddr"
)

// gathers all benchmark data from known peers
func benchmarksHandler(peersdb *app.PeersDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o := *peersdb.Orbit
		ctx := context.Background()
		cinfo, err := o.IPFS().Swarm().Peers(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		client := &http.Client{}
		var benchmarks []app.Benchmark
		for _, c := range cinfo {
			ma := c.Address()
			ip, err := extractIPFromMultiaddr(ma)
			if err != nil {
				// TODO : log the error using logChan
				fmt.Print(err)
				continue
			}

			bm, err := getBenchmark(client, ip)
			if err != nil {
				// TODO : log the error ?
				fmt.Print(err)
				continue
			}

			benchmarks = append(benchmarks, bm)
		}
		benchmarks = append(benchmarks, *peersdb.Benchmark)

		// convert data to json
		jsonData, err := json.Marshal(benchmarks)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// send response
		w.WriteHeader(http.StatusOK)
		w.Write(jsonData)
	}
}

func getBenchmark(client *http.Client, peerIP string) (app.Benchmark, error) {
	var bm app.Benchmark

	bmReq := app.Request{Method: app.BENCHMARK, Args: []string{}}
	jsonData, err := json.Marshal(bmReq)
	if err != nil {
		return bm, err
	}

	// send get benchmark request
	cmdPath := "http://" + peerIP + ":8080/peersdb/command"
	req, err := http.NewRequest("POST", cmdPath, bytes.NewBuffer(jsonData))
	if err != nil {
		return bm, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return bm, err
	}
	defer resp.Body.Close()

	// read response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return bm, err
	}

	// unmarshal response
	err = json.Unmarshal(body, &bm)
	if err != nil {
		return bm, err
	}

	return bm, nil
}

func extractIPFromMultiaddr(maddr multiaddr.Multiaddr) (string, error) {
	re := regexp.MustCompile(`/ip4/(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	match := re.FindStringSubmatch(maddr.String())

	if len(match) >= 2 {
		return match[1], nil
	}

	return "", fmt.Errorf("No ip found in ma " + maddr.String())
}

// // helper function to extract the IP from multiaddr format
// func extractIPFromMultiaddr(maddr multiaddr.Multiaddr) (net.IP, error) {
// 	parts := maddr.String()
// 	protoEndIndex := strings.Index(parts, "/")
// 	if protoEndIndex == -1 {
// 		return nil, fmt.Errorf("failed to extract IP address from Multiaddr: invalid format")
// 	}
//
// 	ipStr := parts[:protoEndIndex]
// 	ip := net.ParseIP(ipStr)
// 	if ip == nil {
// 		return nil, fmt.Errorf("failed to extract IP address from Multiaddr: invalid IP")
// 	}
//
// 	return ip, nil
// }

func commandHandler(reqChan chan<- app.Request,
	resChan <-chan interface{}) http.HandlerFunc {

	type HTTPRequest struct {
		Method app.Method `json:"method"`
		Args   []string   `json:"args"`
		File   string     `json:"file"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// only accepting post requests
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// parse the request body
		var req HTTPRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// post request expects a file instead of the path
		serviceReq := app.Request{
			Method: req.Method,
			Args:   req.Args,
		}
		if serviceReq.Method == app.POST {
			decoded, err := base64.StdEncoding.DecodeString(req.File)
			if err != nil {
				// TODO : use log
				fmt.Println("Error decoding Base64:", err)
				return
			}
			serviceReq.Args = append(serviceReq.Args, string(decoded))
		}

		// send request
		// TODO : we need to do an argument count check here aswell
		reqChan <- serviceReq

		// await response
		res := <-resChan

		// convert data to json
		jsonData, err := json.Marshal(res)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// send response
		w.WriteHeader(http.StatusOK)
		w.Write(jsonData)
	}
}

func ServeHTTP(peersdb *app.PeersDB, reqChan chan app.Request,
	resChan chan interface{}, logChan chan app.Log) {

	server := http.NewServeMux()

	// middleware to handle CORS headers and preflight requests
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			logChan <- app.Log{app.Info, "Received HTTP request from " + ip}

			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	// register command handler which allows to run commands similar to the shell
	server.Handle("/peersdb/command", mw(commandHandler(reqChan, resChan)))

	// register benchmarks handler which is specific for this API because it's
	// used to gather all peers data
	if *config.FlagBenchmark {
		server.Handle("/peersdb/benchmarks", mw(benchmarksHandler(peersdb)))
	}

	// start the HTTP server
	logChan <- app.Log{app.Info, "Starting HTTP Server"}
	http.ListenAndServe(":"+*config.FlagHTTPPort, server)
}
