#!/bin/sh

# This file may be used by script_k8s.yaml to run a simple post action on the 
# network, starting with the root node and posting a file from C3O.

# TODO : git clone to post a file from C3P

# post a file
file_content=$(cat ./samplefile.txt | base64)
curl --location 'http://'$ROOTIP':8080/peersdb/command' \
    --header 'Content-Type: application/json' \
    --data '{
      "method": {
        "argcnt": 1,
        "cmd": "post"
      },
      "args": [],
      "file": "'"$file_content"'"
    }'
