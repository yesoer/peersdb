package config

import "flag"

var FlagShell = flag.Bool("shell", false, "enable shell interface")
var FlagHTTP = flag.Bool("http", false, "enable http interface")

var FlagExp = flag.Bool("experimental", true, "enable experimental features")
var FlagPort = flag.String("port", "4001", "configure application port")
var FlagRepo = flag.String("repo", "peersdb", "configure the repo/directory name for the ipfs node")
var FlagDevLogs = flag.Bool("devlogs", false, "enable development level logging")
var FlagRoot = flag.Bool("root", false, "creating a root node means creating a new datastore")
