# peersdb
A distributed p2p database, based on orbitdb but extend with blockchain technology to provide data integrity. Intended for the sharing of datasets for model training

# Usage

```shell
go run main.go
```

## Flags

| Flag           | Description | Default |
|----------------|-------------|---------|
| -experimental  | enables kubo experimental features | true |
| -devlogs | enables development level logging | false |
| -port | sets the application port | 4001 |
| -shell | enables the shell interface | false |
| -root | makes this node a root node meaning it will create it's own datastore | false |
| -repo | configure the repo/directory name for the ipfs node | peersdb |

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

# Architecture

## Replication

A new peersdb instance could be a root instance. The root instance creates a 
"transactions" orbitdb EventLog store. A new non-root instance will start and
when it's connected to root it will replicate said store. From now on they 
will replicate via events. If a node restarts they will try to load the datastore
from disk.
