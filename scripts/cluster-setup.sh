#!/usr/bin/env bash

# Copyright 2018 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

echo "Running cluster setup"

echo "Installing api server v0.1-alpha.24"
# Instructions from https://github.com/kubernetes-incubator/apiserver-builder/blob/master/docs/installing.md
# WARNING: Using v0.1-alpha.24 release right now. Higher ones should also work, but you might have to update the vendor libraries with:
# $ apiserver-boot update vendor
#
$(
  sudo rm -fr /usr/local/apiserver-builder
  sudo mkdir -p /usr/local/apiserver-builder
  cd /usr/local/apiserver-builder
  sudo wget -q https://github.com/kubernetes-incubator/apiserver-builder/releases/download/v0.1-alpha.24/apiserver-builder-v0.1-alpha.24-linux-amd64.tar.gz \
    -O apiserver-builder-v0.1-alpha.24-linux-amd64.tar.gz
  sudo tar zxf apiserver-builder-v0.1-alpha.24-linux-amd64.tar.gz
)

echo "Installing/updating Bazel"
sudo apt-get update
sudo apt-get install bazel

echo "Setting Bazel up to be able to communicate with GCR"
gcloud --quiet components install docker-credential-gcr || echo "You may need to download and install Cloud SDK separately instead of using the system's package manager"
docker-credential-gcr configure-docker --overwrite

echo "Installing Kubernetes CLI (kubectl)"
gcloud --quiet components install kubectl || echo "You may need to download and install Cloud SDK separately instead of using the system's package manager"

if [[ $(kubectl config current-context) == "minikube" ]]; then
  echo "Installing kube-registry"
  $(
    mkdir -p config
    cd config
    wget -q https://gist.githubusercontent.com/coco98/b750b3debc6d517308596c248daf3bb1/raw/6efc11eb8c2dce167ba0a5e557833cc4ff38fa7c/kube-registry.yaml \
      -O kube-registry.yaml
  )
  kubectl create -f config/kube-registry.yaml
  echo "Downloading etcd"
  $(
    ETCD_VER=v3.2.9
    ETCD_DIR="$HOME/etcd-install"
    ETCD_TARGZ="etcd-${ETCD_VER}-linux-amd64.tar.gz"
    rm -fr "${ETCD_DIR}"
    mkdir -p "${ETCD_DIR}"
    cd "${ETCD_DIR}"
    curl -L "https://storage.googleapis.com/etcd/${ETCD_VER}/${ETCD_TARGZ}" \
      -o "/tmp/${ETCD_TARGZ}"
    tar xzf "/tmp/${ETCD_TARGZ}" -C "${ETCD_DIR}" --strip-components=1
  )
fi

echo "Downloading Istio 0.2.6 locally"
$(
  rm -fr ~/istio-install
  mkdir -p ~/istio-install
  cd ~/istio-install
  wget -q https://github.com/istio/istio/releases/download/0.2.6/istio-0.2.6-linux.tar.gz \
    -O istio-0.2.6-linux.tar.gz
  tar zxf istio-0.2.6-linux.tar.gz
)

echo "Adding a clusterrolebinding"
# Add a clusterrolebinding making you a cluster admin. This missed the install guide in the new documentation (https://github.com/istio/issues/issues/4).
#
# If this step fails (because there already exists a role for it for example, you might need to update it.
# $ kubectl get clusterrolebinding cluster-admin -oyaml > /tmp/cluster-admin
# Then manually edit the Roles, by adding an entry (this example assumes it had Group system:masters) already in it, so the subjects section would look like so:
# subjects:
# - apiGroup: rbac.authorization.k8s.io
#   kind: Group
#   name: system:masters
# - apiGroup: rbac.authorization.k8s.io
#   kind: User
#   name: vaikas@google.com
# And then reapply the rules:
# $ kubectl update clusterrolebinding -f /tmp/cluster-admin
#
kubectl create clusterrolebinding cluster-admin-binding \
    --clusterrole=cluster-admin \
    --user=$USER@google.com

echo "Installing Istio in cluster"
kubectl apply -f ~/istio-install/istio-0.2.6/install/kubernetes/istio.yaml

echo "Installing WarmImage in cluster"
kubectl create namespace warm-image
curl https://raw.githubusercontent.com/mattmoor/warm-image/master/release.yaml \
  | kubectl --namespace=warm-image create -f -


echo "Done with cluster setup"
echo
echo "Build and deploy controller with ./scripts/deploy-controller.sh"
echo
