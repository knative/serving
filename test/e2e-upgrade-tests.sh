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

# You can specify the version to run against with the --version argument
# (e.g. --version v0.7.0). If this argument is not specified, the script will
# run against the latest tagged version on the current branch.

source $(dirname $0)/e2e-common.sh

# Latest serving release. If user does not supply this as a flag, the latest
# tagged release on the current branch will be used.
LATEST_SERVING_RELEASE_VERSION=$(git describe --match "v[0-9]*" --abbrev=0)

function install_latest_release() {
  header "Installing Knative latest public release"
  local url="https://github.com/knative/serving/releases/download/${LATEST_SERVING_RELEASE_VERSION}"
  local yaml="serving.yaml"

  local RELEASE_YAML="$(mktemp)"
  wget "${url}/${yaml}" -O "${RELEASE_YAML}" \
      || fail_test "Unable to download latest Knative release."

  install_knative_serving "${RELEASE_YAML}" \
      || fail_test "Knative latest release installation failed"
  wait_until_pods_running knative-serving
}

function install_head() {
  header "Installing Knative head release"
  install_knative_serving || fail_test "Knative head release installation failed"
  wait_until_pods_running knative-serving
}

function knative_setup() {
  # Build Knative to generate Istio manifests from HEAD for install_latest_release
  # We do it here because it's a one-time setup
  build_knative_from_source
  install_latest_release
}

# Script entry point.

initialize $@ --skip-istio-addon

# TODO(#2656): Reduce the timeout after we get this test to consistently passing.
TIMEOUT=10m

header "Running preupgrade tests"

go_test_e2e -tags=preupgrade -timeout=${TIMEOUT} ./test/upgrade \
  --resolvabledomain=$(use_resolvable_domain) || fail_test

header "Starting prober test"

# Remove this in case we failed to clean it up in an earlier test.
rm -f /tmp/prober-signal

go_test_e2e -tags=probe -timeout=${TIMEOUT} ./test/upgrade \
  --resolvabledomain=$(use_resolvable_domain) &
PROBER_PID=$!
echo "Prober PID is ${PROBER_PID}"

install_head

header "Running postupgrade tests"
go_test_e2e -tags=postupgrade -timeout=${TIMEOUT} ./test/upgrade \
  --resolvabledomain=$(use_resolvable_domain) || fail_test

install_latest_release

header "Running postdowngrade tests"
go_test_e2e -tags=postdowngrade -timeout=${TIMEOUT} ./test/upgrade \
  --resolvabledomain=$(use_resolvable_domain) || fail_test

# The prober is blocking on /tmp/prober-signal to know when it should exit.
#
# This is kind of gross. First attempt was to just send a signal to the go test,
# but "go test" intercepts the signal and always exits with a non-zero code.
echo "done" > /tmp/prober-signal

header "Waiting for prober test"
wait ${PROBER_PID} || fail_test "Prober failed"

success
