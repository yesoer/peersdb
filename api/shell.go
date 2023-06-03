package api

import (
	"bufio"
	"errors"
	"os"
	"peersdb/app"
	"strings"
)

// Method defines simple schemas for actions to perform on the db
type Method struct {
	Cmd    string
	ArgCnt int
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
	Method Method
	Args   []string
}

// checks whether a string list matches a method definition and forwards the
// resulting request
// TODO : wait for response ? could/should that be a method on Request struct ?
//
//	because it's a very repetitive task
func processReq(cmdList []string, method Method,
	reqChan chan Request,
	resChan chan interface{},
	logChan chan app.Log) {

	if len(cmdList) != method.ArgCnt+1 {
		logChan <- app.Log{
			Type: app.RecoverableErr,
			Data: errors.New("double check the given args")}
		return
	}

	// send request
	reqChan <- Request{method, cmdList[1:]}

	// await response and log it
	res := <-resChan
	logChan <- app.Log{Type: app.Print, Data: res}
	logChan <- app.Log{Type: app.Print, Data: "\n"}
}

// start listening for commands, implements the api for the user
func Shell(peersDB *app.PeersDB,
	reqChan chan Request,
	resChan chan interface{},
	logChan chan app.Log) {

	logChan <- app.Log{
		Type: app.Info,
		Data: "Starting shell"}

	for {
		// read the shell input
		reader := bufio.NewReader(os.Stdin)
		logChan <- app.Log{Type: app.Print, Data: ">"}
		cmd, err := reader.ReadString('\n')
		if err != nil {
			logChan <- app.Log{Type: app.RecoverableErr, Data: err}
			continue
		}

		// try to match the command and if successful publish it
		cmd = strings.TrimSpace(cmd)
		cmdList := strings.Split(cmd, " ")
		switch cmdList[0] {
		case GET.Cmd:
			processReq(cmdList, GET, reqChan, resChan, logChan)
		case POST.Cmd:
			processReq(cmdList, POST, reqChan, resChan, logChan)
		case CONNECT.Cmd:
			processReq(cmdList, CONNECT, reqChan, resChan, logChan)
		case QUERY.Cmd:
			processReq(cmdList, QUERY, reqChan, resChan, logChan)
		default:
			logChan <- app.Log{
				Type: app.RecoverableErr,
				Data: errors.New("command not supported")}
			continue
		}

	}
}
