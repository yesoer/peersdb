#!/bin/sh

# this file is supposed to be used by script_k8s.yaml

# post a file
curl --location 'http://'$ROOTIP':8080/peersdb/command' \
    --header 'Content-Type: application/json' \
    --data '{
      "method": {
        "argcnt": 1,
        "cmd": "post"
      },
      "args": [
        ""
      ]
    }'
