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

TARGET=$(kubectl config current-context)
if [[ "${TARGET}" != "minikube" ]]; then
  TARGET="project ${PROJECT_ID?}"
fi

echo "Deploying controller to ${TARGET}"

./scripts/cleanup-resources.sh
./scripts/cleanup-controller.sh

echo "Clearing the discovery cache"
rm -rf ~/.kube/cache/discovery

APISERVER_BOOT=/usr/local/apiserver-builder/bin/apiserver-boot
LOCAL_REGISTRY=localhost:5000

./scripts/deploy-queue.sh

if [[ "${TARGET}" == "minikube" ]]; then
  echo "Starting local registry at ${LOCAL_REGISTRY}"
  registry=$(kubectl get po -n kube-system | grep kube-registry-v0 | awk '{print $1;}')
  kubectl port-forward --namespace kube-system "${registry}" 5000:5000 2>&1 > "/tmp/${registry}.log" &
  echo "Deploying controller"
  rm -f config/apiserver.yaml config/certificates/*
  my_ip=$(ifconfig docker0 | grep inet | cut -f2 -d: | cut -f1 -d' ')
  "${APISERVER_BOOT}" build config --local-minikube --name appresources --namespace default \
    --local-ip "${my_ip}" --image "${LOCAL_REGISTRY}/$USER/demo/controller-istio-upgraded:latest" \
    --controller-args "-registry=${LOCAL_REGISTRY}" --controller-args "-repository=$USER"
  kubectl create -f config/apiserver.yaml
  sudo killall etcd || true
  PATH=$PATH:$HOME/etcd-install "${APISERVER_BOOT}" run local-minikube 2>&1 > /tmp/apiserver-local-minikube.log &
else
  echo "Deploying controller"
  "${APISERVER_BOOT}" run in-cluster --name appresources --namespace default \
    --image us.gcr.io/$PROJECT_ID/demo/controller-istio-upgraded:latest \
    --controller-args "-registry=us.gcr.io" --controller-args "-repository=${PROJECT_ID}"
fi

echo "Warming up sidecar/base images"
kubectl create -f sample/warmimages.yaml

echo "Restarting controller with more memory if necessary"
sed -i 's/20Mi/40Mi/' ./config/apiserver.yaml
sed -i 's/30Mi/50Mi/' ./config/apiserver.yaml
kubectl apply -f config/apiserver.yaml
