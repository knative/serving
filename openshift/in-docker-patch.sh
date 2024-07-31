#!/usr/bin/env bash

set -x
env

git apply ./openshift/performance/patches/*

go get github.com/elastic/go-elasticsearch/v7@v7.17.10
go get github.com/opensearch-project/opensearch-go@v1.1.0

./hack/update-deps.sh

git add .
git commit -m "openshift perf update"

git apply ./openshift/patches/001-object.patch
git apply ./openshift/patches/002-mutemetrics.patch
git apply ./openshift/patches/003-routeretry.patch

git add .
git commit -m "apply reverted patches"