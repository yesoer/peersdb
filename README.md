# PeersDB

A distributed p2p database, based on orbitdb but extend with blockchain technology to provide data integrity. Intended for the sharing of datasets for model training

## Table of Contents
- [Get Started](#get-started)
  - [Flags](#flags)
- [Contribution](#contribution)
  - [Debugging](#debugging)
- [Architecture](#architecture)
  - [Store Replication](#store-replication)
  - [IPFS Replication](#ipfs-replication)
  - [Validation](#validation)
- [APIs](#apis)
  - [Shell](#shell)
  - [HTTP](#http)
- [Evaluation](#evaluation)

# Get Started

```shell
go run main.go
```

## Flags
You may use the following flags to configure your peersdb instance.

| Flag           | Description | Default |
|----------------|-------------|---------|
| -shell | enables the shell interface | false |
| -http | enables the http interface | false |
| -ipfs-port | sets the ipfs port | 4001 |
| -http-port | sets the http port | 8080 |
| -experimental  | enables kubo experimental features | true |
| -repo | configure the repo/directory name for the ipfs node | peersdb |
| -devlogs | enables development level logging | false |
| -root | makes this node a root node meaning it will create it's own datastore | false |
| -download-dir | configure where to store downloaded files etc. | ~/Downloads/ |
| -full-replica | enable full data replication through ipfs pinning | false |
| -bootstrap    | set a bootstrap peer to connect to on startup | "" |
| -benchmark    | enables benchmarking on this node | false |

There is also a persitent config file but you probably don't want to change 
anything in there.

# Contribution :

**Branch naming** should look like this
`<type>/<name>`
where words in "name" are separated by '-'
and type is one of the following (extend if needed)

| type | when to use      |
|------|------------------|
| feat | any new features |
| maintenance | any work on docs, git workflows, tests etc. |
| refactor | when refactoring existing parts of the application |
| fix  | bug fixes        |
| test | testing environments/throwaway branches |

More specific distinction happens in **commit messages** which should be structured
as follows :

```
<type>(<scope>): <subject>
```

**type**
Must be one of the following:

* **feat**: A new feature
* **fix**: A bug fix
* **docs**: Documentation only changes
* **style**: Changes that do not affect the meaning of the code (white-space, formatting, missing
  semi-colons, etc)
* **refactor**: A code change that neither fixes a bug nor adds a feature
* **perf**: A code change that improves performance
* **test**: Adding missing or correcting existing tests
* **chore**: Changes to the build process or auxiliary tools and libraries such as documentation
  generation

**scope** means the part of the software, which usually will be best identified by the package name.

**subject** gives a short idea of what was done/what the intend of the commit is.

As for the **commit body** there is no mandatory structure as of now.

**Issues and Pull Requests** for now will not have any set guidelines.

As a rule of thumb for **merging** make sure to rebase before doing so.

## Debugging

Since I had a rough start here is some help on how to use [delve](https://github.com/go-delve/delve) for debugging.

From the root of this repo use the following command to start debugging (change the flags as needed) :
```
dlv debug peersdb -- -http -benchmark -http-port 8001 -ipfs-port 4001 -repo peersdb1 -root=true -devlogs 
```

Another difficulty was setting breakpoints in dependencies.
Here is an example for setting a breakpoint in the basestores handleEventWrite method :
```
(dlv) b berty.tech/go-orbit-db/stores/basestore.(*BaseStore).handleEventWrite
```

The response for the command above will also include your local paths for those dependencies, so you can set breakpoints by line now :
```
(dlv) b /Users/<username>/go/pkg/mod/berty.tech/go-orbit-db@v1.21.0/stores/basestore/base_store.go:853
```

# Architecture

## Store Replication

The contributions store, holds file-ipfs-paths. It needs to be replicated for peers to know which data is available.

A new peersdb instance could be a root instance. The root instance creates a 
"transactions" orbitdb EventLog store. A new non-root instance will start and
when it's connected to root it will replicate said store. From now on they 
will replicate via events. If a node restarts they will try to load the datastore
from disk.

## IPFS Replication

IPFS Replication is achieved through IPFS pinning. It needs to be enabled via the `full-replica` flag.
Pinning is triggered whenever the orbitdb contribution store receives data i.e. the replicated event is triggered.

## Validation

Files need to be validated. The approach is as follows :

when peers add new contribution blocks they try validating the data 
  - implemented by `awaitWriteEvent`

each peer keeps their own validation records
  - in a persistent docstore called `validations`

when someone wants to know (implemented in `isValid` which uses `accValidations`)
1. they retrieve all info (signed by the sender) via pubsub
    - announce their wish via topic : "validation" with message data : their id + the files cid
    - receive votes via topic : their id + the files cid
        - peers start listening for those requests in `awaitValidationReq`
2. count votes themselves
    - when more than half of the connected peers have voted for valid, it's valid
    else the node validates the data itself
3. persist the result
    - in the `validations` store mentioned earlier

# APIs

## Shell

Shell commands look like this :
`<command> <arg1> <arg2> ...`

### get

**Description :**
Download ipfs content by it's ipfs path. The destination can be configured via the `-download-dir` flag.
Ipfs paths can be retrieved via the `query` command.

**Args :**

| Description                   | Example | 
|-------------------------------|------------------------------------------------------------------------------|
| The path of some ipfs content | `/ipfs/4001/p2p/QmRQSrmFNEWx7qKF5jrdLJ4oS8dZzYpTKDoAKoDzL3zXr7` |

**Returns :**
A status string.

### connect 

**Description :**
Manually connect to other peers .

**Args :**

| Description                   | Example | 
|-------------------------------|------------------------------------------------------------------------------|
| The path of another ipfs node | `/ip4/127.0.0.1/tcp/4001/p2p/QmRQSrmFNEWx7qKF5jrdLJ4oS8dZzYpTKDoAKoDzL3zXr7` |

**Returns :**
A status string.

### post

**Description :**
Adds a file to the local ipfs node and stores the contribution block in the eventlog

**Args :**

| Description  |   Example | 
|--------------|-----------|
| the filepath | ./main.go |

**Returns :**
A status string.

### query

**Description :**
Queries the eventlog for all entries

**Args :**

-

**Returns :**
A results list.

## HTTP

### POST  /peersdb/command

Execute a command.

**Body :**
```
{
  method: {
    argcnt: int
    cmd: string
  }
  args: [
    string
  ]
}
```

cmd identifies the same commands as described under [Shell](#shell). They also receive the same arguments.
The only **exception** ist the "POST" command, where one has to provide a base64 encoded file instead under the "file" key.

# Evaluation

The `eval` folder contains everything we need for some predefined scenarios on a configurable cluster of nodes. 
To make it short : we use Helm charts to deploy docker containers on kubernetes, and scripts of http requests to define certain workflows.
This allows us to easily evaluate how well peersdb handles certain tasks like replication. 
Our runs were largely executed on GCP but if you want to dabble around with them locally we'd advice to use kind.

> Note : the dockerfile aswell as the helm chart are NOT fit for production usage.

If you want to build your own docker image you can do it like this :
```
docker buildx build --platform linux/amd64,linux/arm64 --push -t yesoer/peersdb:latest -f eval/dockerfile .
```
this way the image will be built for amd64 and arm64 architectures and directly pushed to the configured image repository.
If you don't care about multi-architecture builds you may simply use : 
```
docker build -t yesoer/peersdb:latest -f eval/dockerfile .
```

To deploy the helm chart(s) :
```
helm install -f ./eval/root-peer-values.yaml peersdb-root ./eval/helm
helm install -f ./eval/peer-values.yaml peersdb-peers ./eval/helm
```

The workflows executed on the cluster use the HTTP API and are defined programatically.
To run them, we will copy whichever script we want into the container and execute it manually.

```
kubectl cp ./eval/workflows/ default/<root pod>:/app/
kubectl exec -it <root pod> -- /bin/sh
```

If you want to change the workflow, simply change the script referenced in the yaml.

For deploying on specific nodes (relevant when evaluating in a cluster with nodes in different regions)
use the following approach :
```
kubectl label nodes <you node> region=<your node's region>
helm install -f ./eval/peer-values.yaml peersdb-peers ./eval/helm --set nodeSelector.region=<your nodes region>
```
