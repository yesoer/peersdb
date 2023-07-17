#!/bin/sh

# sourcing utils
. ./utils.sh

# setup prerequisits
echo "Setup"
install_tools
clone_c3o

# zip dataset
echo "Zip c3o dataset"
apk add zip 
zip -r dataset.zip "./c3o-experiments"

# post the scout dataset
echo "Post dataset.zip"
post "dataset.zip"

# wait for all stores to replicate
sleep 1m

# gather and store benchmarks
echo "Get Benchmarks"
gather_benchmarks > benchmarks.json
