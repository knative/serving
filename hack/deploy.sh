#!/bin/bash

# Copyright 2018 Google, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit

: ${PROJECT_ID:="elafros-environments"}
readonly PROJECT_ID
readonly K8S_CLUSTER_NAME=${1:?"First argument must be the kubernetes cluster name."}
readonly K8S_CLUSTER_ZONE=us-central1-a
readonly K8S_CLUSTER_VERSION=1.9.6-gke.1
readonly K8S_CLUSTER_MACHINE=n1-standard-8
readonly K8S_CLUSTER_NODES=5
readonly ISTIO_VERSION=0.6.0
readonly ELAFROS_RELEASE=https://storage.googleapis.com/elafros-releases/latest/release.yaml

readonly ELAFROS_ROOT=$(dirname ${BASH_SOURCE})/..
export ISTIO_VERSION

# TODO(adrcunha): Factor out common functions with e2e-tests.sh.

function header() {
  echo "*************************************************"
  echo "** $1"
  echo "*************************************************"
}

function wait_until_running() {
  echo -n "Waiting until all pods in namespace $1 are up"
  for i in {1..150}; do  # timeout after 5 minutes
    local not_running=$(kubectl get pods -n $1 | grep -v NAME | grep -v Running | wc -l)
    if [[ ${not_running} == 0 ]]; then
      echo -e "\nAll pods are up:"
      kubectl get pods -n $1
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for pods to come up"
  return 1
}

cd ${ELAFROS_ROOT}

clusters=$(gcloud --project=${PROJECT_ID} container clusters list \
  --zone=${K8S_CLUSTER_ZONE} | grep ${K8S_CLUSTER_NAME} | wc -l)
if [[ -n ${clusters} && ${clusters} != 0 ]]; then
  header "Deleting previous cluster ${K8S_CLUSTER_NAME} in ${PROJECT_ID}"
  gcloud -q --project=${PROJECT_ID} container clusters delete \
    --zone=${K8S_CLUSTER_ZONE} ${K8S_CLUSTER_NAME}
fi

header "Creating cluster ${K8S_CLUSTER_NAME} in ${PROJECT_ID}"
gcloud --project=${PROJECT_ID} container clusters create \
  --cluster-version=${K8S_CLUSTER_VERSION} \
  --zone=${K8S_CLUSTER_ZONE} \
  --scopes=cloud-platform \
  --machine-type=${K8S_CLUSTER_MACHINE} \
  --enable-autoscaling --min-nodes=1 --max-nodes=${K8S_CLUSTER_NODES} \
  ${K8S_CLUSTER_NAME}

header "Fetching istio ${ISTIO_VERSION}"
rm -fr istio-${ISTIO_VERSION}
curl -L https://git.io/getLatestIstio | sh -

header "Setting cluster admin"
# Get the password of the admin and use it to set the cluster admin, as
# the service account (or the user) might not have the necessary permission.
password=$(gcloud --project=${PROJECT_ID} container clusters describe \
  ${K8S_CLUSTER_NAME} --zone=${K8S_CLUSTER_ZONE} | \
  grep password | cut -d' ' -f4)
kubectl --username=admin --password=${password} \
  create clusterrolebinding cluster-admin-binding \
  --clusterrole=cluster-admin \
  --user=$(gcloud config get-value core/account)

pushd istio-${ISTIO_VERSION}/install/kubernetes

header "Installing istio"
kubectl apply -f istio.yaml
wait_until_running istio-system

header "Enabling automatic sidecar injection in Istio"
./webhook-create-signed-cert.sh \
  --service istio-sidecar-injector \
  --namespace istio-system \
  --secret sidecar-injector-certs
kubectl apply -f istio-sidecar-injector-configmap-release.yaml
cat ./istio-sidecar-injector.yaml | \
  ./webhook-patch-ca-bundle.sh > istio-sidecar-injector-with-ca-bundle.yaml
kubectl apply -f istio-sidecar-injector-with-ca-bundle.yaml
rm ./istio-sidecar-injector-with-ca-bundle.yaml
kubectl label namespace default istio-injection=enabled
wait_until_running istio-system

popd

header "Installing Elafros"
# This will fail, ignore (see https://github.com/elafros/install/issues/13)
kubectl apply -f ${ELAFROS_RELEASE} || true
# This should pass (see https://github.com/elafros/install/issues/13)
kubectl apply -f ${ELAFROS_RELEASE}

wait_until_running ela-system

header "Elafros deployed successfully to ${K8S_CLUSTER_NAME}"
