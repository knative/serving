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

# Helper functions.
function dump_extra_cluster_state() {
  echo ">>> Routes:"
  kubectl get routes -o yaml --all-namespaces
  echo ">>> Configurations:"
  kubectl get configurations -o yaml --all-namespaces
  echo ">>> Revisions:"
  kubectl get revisions -o yaml --all-namespaces
  echo ">>> Knative Serving controller log:"
  kubectl -n knative-serving logs $(get_app_pod controller knative-serving)
  echo ">>> Knative Serving autoscaler log:"
  kubectl -n knative-serving logs $(get_app_pod autoscaler knative-serving)
  echo ">>> Knative Serving activator log:"
  kubectl -n knative-serving logs $(get_app_pod activator knative-serving)
}

function publish_test_images() {
  echo ">> Publishing test images"
  image_dirs="$(find ${REPO_ROOT_DIR}/test/test_images -mindepth 1 -maxdepth 1 -type d)"
  for image_dir in ${image_dirs}; do
    ko publish -P "github.com/knative/serving/test/test_images/$(basename ${image_dir})"
  done
}

# Deletes everything created on the cluster including all knative and istio components.
function teardown() {
  delete_everything
}

# Script entry point.

initialize $@

# Fail fast during setup.
set -o errexit
set -o pipefail

header "Setting up environment"
create_everything
publish_test_images

# Handle test failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

wait_until_cluster_up

# Run the tests

header "Running tests"
kubectl create namespace serving-tests
go_test_e2e -timeout=20m ./test/conformance ./test/e2e || fail_test

success
