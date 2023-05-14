package main

import (
	"bufio"
	"errors"
	"os"
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
func fwdCmd(cmdList []string, method Method, reqChan chan Request, logChan chan Log) {
	if len(cmdList) != method.ArgCnt+1 {
		logChan <- Log{RecoverableErr, errors.New("double check the given args")}
		return
	}

	reqChan <- Request{method, cmdList[1:]}
}

// start listening for commands, implements the api for the user
func shell(peersDB *PeersDB, reqChan chan Request, resChan chan interface{}, logChan chan Log) {
	logChan <- Log{Info, "Starting shell"}
	for {
		// read the shell input
		reader := bufio.NewReader(os.Stdin)
		logChan <- Log{Print, ">"}
		cmd, err := reader.ReadString('\n')
		if err != nil {
			logChan <- Log{RecoverableErr, err}
			continue
		}

		// try to match the command and if successful publish it
		cmd = strings.TrimSpace(cmd)
		cmdList := strings.Split(cmd, " ")
		switch cmdList[0] {
		case GET.Cmd:
			fwdCmd(cmdList, GET, reqChan, logChan)
		case POST.Cmd:
			fwdCmd(cmdList, POST, reqChan, logChan)
		case CONNECT.Cmd:
			fwdCmd(cmdList, CONNECT, reqChan, logChan)
		case QUERY.Cmd:
			fwdCmd(cmdList, QUERY, reqChan, logChan)
		default:
			logChan <- Log{RecoverableErr, errors.New("command not supported")}
			continue
		}
	}
}
