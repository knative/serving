#!/usr/bin/env bash

set -e

echo "Downloading vertical pod autoscaler..."
go get -d -u k8s.io/autoscaler/vertical-pod-autoscaler/...

echo "Installing vertical pod autoscaler..."
cd ${GOPATH?}/src/k8s.io/autoscaler/vertical-pod-autoscaler
git checkout vpa-release-0.1
./hack/vpa-up.sh

