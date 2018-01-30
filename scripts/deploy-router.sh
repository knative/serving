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

echo "Deploying router to project ${PROJECT_ID?}"
pushd pkg/router

echo "Deleting router pod if it exists"
kubectl delete -f router-pod.yaml --ignore-not-found=true

echo "Building router"
go build router.go
docker build -t "us.gcr.io/${PROJECT_ID}/router" .
gcloud docker -- push "us.gcr.io/${PROJECT_ID}/router"

kubectl get namespace default-ela || kubectl create namespace default-ela

echo "Deploying router service if it doesn't exist"
kubectl -n default-ela get service router-service || kubectl create -f router-service.yaml

echo "Deploying router pod"
kubectl create -f router-pod.yaml
