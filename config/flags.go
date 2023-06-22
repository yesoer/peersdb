package config

import "flag"

var FlagShell = flag.Bool("shell", false, "enable shell interface")
var FlagHTTP = flag.Bool("http", false, "enable http interface")

var FlagIPFSPort = flag.String("ipfs-port", "4001", "configure ipfs port")
var FlagHTTPPort = flag.String("http-port", "8080", "configure http port")

var FlagExp = flag.Bool("experimental", true, "enable ipfs experimental features")
var FlagRepo = flag.String("repo", "peersdb", "configure the repo/directory name for the ipfs node")
var FlagDevLogs = flag.Bool("devlogs", false, "enable development level logging for orbitdb")
var FlagRoot = flag.Bool("root", false, "creating a root node means it's possible to create a new datastore")
var FlagDownloadDir = flag.String("download-dir", "~/Downloads/", "the destination path for downloaded data")
var FlagFullReplica = flag.Bool("full-replica", false, "pins all added data")
