# !/bin/sh

peers=(
  "europe-west3-b"
  "us-west1-b"
  "southamerica-east1-a"
  "me-west1-b"
  "australia-southeast1-a"
  "asia-east2-a"
)

rootip="10.233.0.50"
rootregion="asia-east2-a"

# first param is the revision
function deploy_peers() {
    # deploy peers
    for region in "${peers[@]}"; do
        helm install -f ../peer-values.yaml peersdb-"$1"-"$region" ./eval/helm \
            --set nodeSelector.enable=true \
            --set nodeSelector.region="$region" \
            --set params.rootIP="$rootip" \
            --set deployment.replicaCount=1
        sleep 30
    done
}

function setup() {
    echo "issue helm installs"
    # deploy root node
    helm install -f ../root-peer-values.yaml peersdb-root ./eval/helm \
        --set nodeSelector.enable=true \
        --set nodeSelector.region=$rootregion \
        --set service.ip=$rootip
  
    # # allow root peer to start
    sleep 60
    deploy_peers 1
}

function teardown() {
    echo "tearing helm installs down"
    helm ls --all --short | xargs -L1 helm uninstall
}

# check the command line argument and call the appropriate function
if [ "$1" == "setup" ]; then
    setup
elif [ "$1" == "expand" ]; then
    if [ -z "$2" ]; then
        echo "missing expand postfix"
        exit
    fi

    deploy_peers $2
elif [ "$1" == "teardown" ]; then
    teardown
else
    echo "Invalid function name. Usage: $0 <function_name>"
fi
