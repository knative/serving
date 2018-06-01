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

source "$(dirname $(readlink -f ${BASH_SOURCE}))/../test/library.sh"

: ${PROJECT_ID:="knative-environments"}
readonly PROJECT_ID
readonly K8S_CLUSTER_NAME=${1:?"First argument must be the kubernetes cluster name."}
readonly K8S_CLUSTER_ZONE=us-central1-a
readonly K8S_CLUSTER_MACHINE=n1-standard-8
readonly K8S_CLUSTER_NODES=5
readonly ISTIO_VERSION=0.6.0
readonly ELAFROS_RELEASE=https://storage.googleapis.com/knative-releases/latest/release.yaml
export ISTIO_VERSION
readonly PROJECT_USER=$(gcloud config get-value core/account)
readonly CURRENT_PROJECT=$(gcloud config get-value project)

function cleanup() {
  gcloud config set project ${CURRENT_PROJECT}
}

cd ${ELAFROS_ROOT_DIR}
trap cleanup EXIT

echo "Using project ${PROJECT_ID} and user ${PROJECT_USER}"
gcloud config set project ${PROJECT_ID}

existing_cluster=$(gcloud container clusters list \
    --zone=${K8S_CLUSTER_ZONE} --filter="name=${K8S_CLUSTER_NAME}")
if [[ -n "${existing_cluster}" ]]; then
  header "Deleting previous cluster ${K8S_CLUSTER_NAME} in ${PROJECT_ID}"
  gcloud -q container clusters delete \
    --zone=${K8S_CLUSTER_ZONE} ${K8S_CLUSTER_NAME}
fi

header "Creating cluster ${K8S_CLUSTER_NAME} in ${PROJECT_ID}"
gcloud --project=${PROJECT_ID} container clusters create \
  --cluster-version=${ELAFROS_GKE_VERSION} \
  --zone=${K8S_CLUSTER_ZONE} \
  --scopes=cloud-platform \
  --machine-type=${K8S_CLUSTER_MACHINE} \
  --enable-autoscaling --min-nodes=1 --max-nodes=${K8S_CLUSTER_NODES} \
  ${K8S_CLUSTER_NAME}

header "Fetching istio ${ISTIO_VERSION}"
rm -fr istio-${ISTIO_VERSION}
curl -L https://git.io/getLatestIstio | sh -

header "Setting cluster admin"
acquire_cluster_admin_role ${PROJECT_USER} ${K8S_CLUSTER_NAME} ${K8S_CLUSTER_ZONE}

pushd istio-${ISTIO_VERSION}/install/kubernetes

header "Installing istio"
kubectl apply -f istio.yaml
wait_until_pods_running istio-system

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
wait_until_pods_running istio-system

popd

header "Installing Elafros"
# Install might fail before succeding, so we retry a few times.
# For details, see https://github.com/knative/install/issues/13
installed=0
for i in {1..10}; do
  kubectl apply -f ${ELAFROS_RELEASE} && installed=1 && break
  sleep 30
done
if (( ! installed )); then
  echo "ERROR: could not install Elafros"
  exit 1
fi

wait_until_pods_running ela-system
wait_until_pods_running build-system

header "Elafros deployed successfully to ${K8S_CLUSTER_NAME}"
