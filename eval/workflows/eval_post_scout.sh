#!/bin/sh

# sourcing utils
. ./utils.sh

# setup prerequisits
echo "Setup"
install_tools
clone_c3o
 clone_scout
apk add zip 

# zip c3o dataset
echo "Zip c3o dataset"
zip -r dataset.zip "./c3o-experiments"

# post the c3o dataset
echo "Post dataset.zip"
post "dataset.zip"

# zip and post subdirectories of scout
cd scout/dataset/osr_single_node/

files=$(ls)
for file in $files; do
    # zip and post
    zip -r $file.zip $file
    post "$file.zip"
done

cd ../../../

# wait for all stores to replicate
sleep 1m

# gather and store benchmarks
echo "Get Benchmarks"
bm=$(gather_benchmarks)
echo $bm

store_bm_csv $bm
echo $bm > benchmark.json
