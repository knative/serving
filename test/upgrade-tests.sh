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
readonly SERVING_RELEASE_YAML=https://github.com/knative/serving/releases/download/v0.2.1/release-no-mon.yaml

# Cribbed from e2e-tests.sh
# TODO(#2320): Remove this.
function publish_test_images() {
  echo ">> Publishing test images"
  image_dirs="$(find ${REPO_ROOT_DIR}/test/test_images -mindepth 1 -maxdepth 1 -type d)"
  for image_dir in ${image_dirs}; do
    ko publish -P "github.com/knative/serving/test/test_images/$(basename ${image_dir})"
  done
}

function install_latest_release() {
  header "Installing latest release"
  set -o errexit
  set -o pipefail
  RELEASE_YAML_OVERRIDE=${SERVING_RELEASE_YAML} install_knative_serving
  set +o errexit
  set +o pipefail
}

function install_head() {
  header "Installing HEAD"
  set -o errexit
  set -o pipefail
  install_knative_serving
  set +o errexit
  set +o pipefail
}

# Deletes everything created on the cluster including all knative and istio components.
function teardown() {
  uninstall_knative_serving
  RELEASE_YAML_OVERRIDE=${SERVING_RELEASE_YAML} uninstall_knative_serving
}

# Script entry point.

initialize $@

header "Setting up environment"
kubectl create namespace serving-tests
publish_test_images

install_latest_release

header "Running preupgrade tests"
go_test_e2e -tags=preupgrade -timeout=5m ./test/upgrade || fail_test

install_head

header "Running postupgrade tests"
go_test_e2e -tags=postupgrade -timeout=5m ./test/upgrade || fail_test

install_latest_release

header "Running postdowngrade tests"
go_test_e2e -tags=postdowngrade -timeout=5m ./test/upgrade || fail_test

success
