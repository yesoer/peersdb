#!/bin/sh

# installs tools needed by the other commands
install_tools() {
    apk update
    apk add git
    apk add curl
    apk add jq
}

# requires git to be present
clone_scout() {
    git clone https://github.com/oxhead/scout.git
}

# requires git to be present
clone_c3o() {
    git clone https://github.com/dos-group/c3o-experiments.git
}

# post some file to the local peersdb instance
# first parameter is the filename
post() {
    # file_content=$(cat $1 | base64)
    file_content=$(cat "$1" | base64 | tr -d '\n')
    curl --location 'http://127.0.0.1:8080/peersdb/command' \
        --header 'Content-Type: application/json' \
        --data '{
          "method": {
            "argcnt": 1,
            "cmd": "post"
          },
          "args": [],
          "file": "'"$file_content"'"
        }'
}

# connect the local peersdb instance to some peer
# the first parameter is the other peers ip
# the second parameter their id
connect() {
    PEERIP=$1
    PEERID=$2
    IPFSADDR="/ip4/$PEERIP/tcp/4001/p2p/$PEERID"
    curl --location 'http://127.0.0.1:8080/peersdb/command' \
        --header 'Content-Type: application/json' \
        --data '{
          "method": {
            "argcnt": 1,
            "cmd": "connect"
          },
          "args": ["'"$IPFSADDR"'"]
        }'
}

# query local peersdb instance
query() {
    curl --location 'http://127.0.0.1:8080/peersdb/command' \
        --header 'Content-Type: application/json' \
        --data '{
          "method": {
            "argcnt": 0,
            "cmd": "query"
          },
          "args": []
        }'
}

# ask local peer to gather benchmark data from all known peers
gather_benchmarks() {
    curl --location 'http://127.0.0.1:8080/peersdb/benchmarks' \
        --header 'Content-Type: application/json'
}

store_bm_csv() {
    # convert JSON data to CSV format
    csv_data=$(echo "$1" | jq -r '.[] | [.bootstrap, .maxc, .minc, .averagec, .region] | @csv')

    # add header row
    header="Bootstrap time, Maximum time for replication, Minimum time for replication, Average replication time,Node Region"

    # create a new file and store the CSV data
    echo "$header" > benchmark_raw.csv
    echo "$csv_data" >> benchmark_raw.csv
}
