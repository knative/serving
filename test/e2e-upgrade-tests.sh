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
# shellcheck disable=SC1090
source "$(dirname "${BASH_SOURCE[0]}")/e2e-common.sh"

# Script entry point.

# Skip installing istio as an add-on.
# Temporarily increasing the cluster size for serving tests to rule out
# resource/eviction as causes of flakiness.
initialize "$@" --skip-istio-addon --min-nodes=4 --max-nodes=4

# We haven't configured these deployments for high-availability,
# so disable the chaos duck.
# TODO(mattmoor): Reconsider this after 0.17 cuts.
disable_chaosduck

# TODO(#2656): Reduce the timeout after we get this test to consistently passing.
TIMEOUT=30m

header "Running upgrade tests"

go_test_e2e -tags=upgrade -timeout=${TIMEOUT} \
  ./test/upgrade \
  --resolvabledomain=$(use_resolvable_domain) || fail_test

# Remove the kail log file if the test flow passes.
# This is for preventing too many large log files to be uploaded to GCS in CI.
rm "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt"
success
