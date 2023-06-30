package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"peersdb/app"
	"peersdb/config"
)

func commandHandler(reqChan chan<- app.Request,
	resChan <-chan interface{}) http.HandlerFunc {

	type HTTPRequest struct {
		Method app.Method `json:"method"`
		Args   []string   `json:"args"`
		File   string     `json:"file"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
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
			// Handle error, for example:
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// send response
		w.WriteHeader(http.StatusOK)
		w.Write(jsonData)
	}
}

func ServeHTTP(reqChan chan app.Request, resChan chan interface{}) {
	server := http.NewServeMux()

	// middleware to handle CORS headers and preflight requests
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			// only accepting post requests
			if r.Method != "POST" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	// register handlers
	server.Handle("/peersdb/command", mw(commandHandler(reqChan, resChan)))

	// start the HTTP server on port 8080
	http.ListenAndServe(":"+*config.FlagHTTPPort, server)
}
