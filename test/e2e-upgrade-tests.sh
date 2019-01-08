#!/usr/bin/env bash

# Copyright 2018 The Knative Authors
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

# This script runs the end-to-end tests against Knative Serving built from source.
# It is started by prow for each PR. For convenience, it can also be executed manually.

# If you already have the *_OVERRIDE environment variables set, call
# this script with the --run-tests arguments and it will start knative in
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname $0)/cluster.sh

# Latest serving release. This is intentionally hardcoded so that we can test
# upgrade/downgrade on release branches (or even arbitrary commits).
#
# Unfortunately, that means we'll need to manually bump this version when we
# make new releases.
#
# Fortunately, that's not *too* terrible, because forgetting to bump this
# version will make tests either:
# 1. Still pass, meaning we can upgrade from earlier than latest release (good).
# 2. Fail, which might be remedied by bumping this version.
readonly LATEST_SERVING_RELEASE_VERSION=0.2.3

function install_latest_release() {
  header "Installing Knative latest public release"
  local url="https://github.com/knative/serving/releases/download/v${LATEST_SERVING_RELEASE_VERSION}"
  # TODO: should this test install istio and build at all, or only serving?
  install_knative_serving \
    "${url}/istio-crds.yaml" \
    "${url}/istio.yaml" \
    "${url}/serving.yaml" \
    || fail_test "Knative latest release installation failed"
}

function install_head() {
  header "Installing Knative head release"
  install_knative_serving || fail_test "Knative head release installation failed"
}

# Deletes everything created on the cluster including all knative and istio components.
function teardown() {
  uninstall_knative_serving
}

# Script entry point.

initialize $@

header "Setting up environment"
publish_test_images || fail_test "one or more test images weren't published"

install_latest_release

# TODO(#2656): Reduce the timeout after we get this test to consistently passing.
TIMEOUT=10m
header "Running preupgrade tests"
go_test_e2e -tags=preupgrade -timeout=$TIMEOUT ./test/upgrade || fail_test

install_head

header "Running postupgrade tests"
go_test_e2e -tags=postupgrade -timeout=$TIMEOUT ./test/upgrade || fail_test

install_latest_release

header "Running postdowngrade tests"
go_test_e2e -tags=postdowngrade -timeout=$TIMEOUT ./test/upgrade || fail_test

success
