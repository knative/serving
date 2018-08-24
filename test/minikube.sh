#!/bin/bash

set -x
#set -e

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/library.sh

echo "I am G$(whoami)"
echo "HOME is $HOME"
echo "I am at $(pwd)"
echo "Path is $PATH"

# Fake sudo
echo "$@" > /usr/bin/sudo
chmod +x /usr/bin/sudo

curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo systemctl enable docker

curl -Lo minikube https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64 && chmod +x minikube

export MINIKUBE_WANTUPDATENOTIFICATION=false
export MINIKUBE_WANTREPORTERRORPROMPT=false
export MINIKUBE_HOME=$HOME
export CHANGE_MINIKUBE_NONE_USER=true
mkdir -p $HOME/.kube
touch $HOME/.kube/config

export KUBECONFIG=$HOME/.kube/config
./minikube stop
./minikube logs

./minikube start --vm-driver=none \
--memory=8192 --cpus=4 \
--kubernetes-version=v1.10.5 \
--bootstrapper=kubeadm \
--extra-config=controller-manager.cluster-signing-cert-file="/var/lib/localkube/certs/ca.crt" \
--extra-config=controller-manager.cluster-signing-key-file="/var/lib/localkube/certs/ca.key" \
--extra-config=apiserver.admission-control="LimitRanger,NamespaceExists,NamespaceLifecycle,ResourceQuota,ServiceAccount,DefaultStorageClass,MutatingAdmissionWebhook"
./minikube logs

# this for loop waits until kubectl can access the api server that Minikube has created
for i in {1..150}; do # timeout for 5 minutes
   kubectl get po &> /dev/null
   if [ $? -ne 1 ]; then
      break
  fi
  sleep 2
done

# kubectl commands are now able to interact with Minikube cluster

export K8S_CLUSTER_OVERRIDE='minikube'
# When using Minikube, the K8s user is your local user.
export K8S_USER_OVERRIDE=$USER

ip route add $(grep ServiceCIDR ~/.minikube/profiles/minikube/config.json | cut -f4 -d\") via $(./minikube ip)
kubectl run minikube-lb-patch --replicas=1 --image=elsonrodriguez/minikube-lb-patch:0.1 --namespace=kube-system

# Switch the current kubectl context to minikube
kubectl config use-context minikube

# Set KO_DOCKER_REPO to a sentinel value for ko to sideload into the daemon.
export KO_DOCKER_REPO="ko.local"

export DOCKER_REPO_OVERRIDE=${KO_DOCKER_REPO}

start_latest_knative_serving

./test/upload-test-images.sh minikube

go test -v -tags=e2e -count=1 ./test/conformance --tag minikube

./minikube stop
