#!/bin/sh

# sourcing utils
. ./utils.sh

# setup prerequisits
echo "Setup"
install_tools
clone_c3o
apk add zip 

# zip c3o dataset
echo "Zip c3o dataset"
zip -r dataset.zip "./c3o-experiments"

# post the c3o dataset
echo "Post dataset.zip"
post "dataset.zip"

# wait for all stores to replicate
sleep 1m

# gather and store benchmarks
echo "Get Benchmarks"
bm=$(gather_benchmarks)
echo $bm

store_bm_csv $bm
