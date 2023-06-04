package api

import (
	"bufio"
	"errors"
	"os"
	"peersdb/app"
	"strings"
)

// checks whether a string list matches a method definition and forwards the
// resulting request
// TODO : wait for response ? could/should that be a method on Request struct ?
//
//	because it's a very repetitive task
func processReq(cmdList []string, method app.Method,
	reqChan chan app.Request,
	resChan chan interface{},
	logChan chan app.Log) {

	if len(cmdList) != method.ArgCnt+1 {
		logChan <- app.Log{
			Type: app.RecoverableErr,
			Data: errors.New("double check the given args")}
		return
	}

	// send request
	reqChan <- app.Request{Method: method, Args: cmdList[1:]}

	// await response and log it
	res := <-resChan
	logChan <- app.Log{Type: app.Print, Data: res}
	logChan <- app.Log{Type: app.Print, Data: "\n"}
}

// start listening for commands, implements the api for the user
func Shell(reqChan chan app.Request,
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
		case app.GET.Cmd:
			processReq(cmdList, app.GET, reqChan, resChan, logChan)
		case app.POST.Cmd:
			processReq(cmdList, app.POST, reqChan, resChan, logChan)
		case app.CONNECT.Cmd:
			processReq(cmdList, app.CONNECT, reqChan, resChan, logChan)
		case app.QUERY.Cmd:
			processReq(cmdList, app.QUERY, reqChan, resChan, logChan)
		default:
			logChan <- app.Log{
				Type: app.RecoverableErr,
				Data: errors.New("command not supported")}
			continue
		}

	}
}
