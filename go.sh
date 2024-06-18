#!/bin/bash
if [[ "$(find vendor/knative.dev/networking -maxdepth 0 -mindepth 0 -type l | wc -l)" == 0 ]]; then
  pushd vendor/knative.dev
  for D in networking pkg; do
    rm -rf ${D}
    ln -s ../../../${D}
  done
  popd
fi
if [ -z "${TAG}" ]; then
  TAG=$(date +%Y%m%d)
fi
export GOFLAGS=-mod=vendor
NAME=$1
set -xeo pipefail
ko publish ./cmd/${NAME} --push=false --local | tee /tmp/go.log
IMAGE=$(tail -n 1 /tmp/go.log)
docker tag ${IMAGE} 724664234782.dkr.ecr.us-east-1.amazonaws.com/library/knative-releases/knative.dev/serving/cmd/${NAME}:${TAG}
if [ -z "${PUSH}" ]; then
  docker push 724664234782.dkr.ecr.us-east-1.amazonaws.com/library/knative-releases/knative.dev/serving/cmd/${NAME}:${TAG}
fi
