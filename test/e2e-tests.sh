#!/bin/bash

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

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/e2e-tests.sh

# Location of istio for the test cluster
readonly ISTIO_YAML=./third_party/istio-0.8.0/istio.yaml

# Helper functions.

function create_istio() {
  echo ">> Bringing up Istio"
  kubectl apply -f ${ISTIO_YAML}
}

function create_monitoring() {
  echo ">> Bringing up monitoring"
  kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-prod \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
}

function create_everything() {
  create_istio
  echo ">> Binging up Serving "
  kubectl apply -f third_party/config/build/release.yaml
  ko apply -f config/
  create_monitoring
}

function delete_istio() {
  echo ">> Bringing down Istio"
  kubectl delete --ignore-not-found=true -f ${ISTIO_YAML}
  kubectl delete clusterrolebinding cluster-admin-binding
}

function delete_monitoring() {
  echo ">> Bringing down monitoring"
  kubectl delete --ignore-not-found=true -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-prod \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common
}

function delete_everything() {
  delete_monitoring
  echo ">> Bringing down Serving"
  ko delete --ignore-not-found=true -f config/
  kubectl delete --ignore-not-found=true -f third_party/config/build/release.yaml
  delete_istio
}

function teardown() {
  delete_everything
}

function dump_extra_cluster_state() {
  echo ">>> Routes:"
  kubectl get routes -o yaml --all-namespaces
  echo ">>> Configurations:"
  kubectl get configurations -o yaml --all-namespaces
  echo ">>> Revisions:"
  kubectl get revisions -o yaml --all-namespaces
  echo ">>> Knative Serving controller log:"
  local controller=$(kubectl get pods -n knative-serving \
    --selector=app=controller --output=jsonpath="{.items[0].metadata.name}")
  kubectl logs $(get_app_pod controller knative-serving)
}

function publish_test_images() {
  echo ">>> Publishing test images"
  image_dirs="$(find ${REPO_ROOT_DIR}/test/*/test_images -mindepth 1 -maxdepth 1 -type d)"
  for image_dir in ${image_dirs}; do
    # This is a bit of a hack, since we don't use $1 (e2e or
    # conformance) in the path. But, when referencing the repo in
    # e2e_flags.go we don't have knowledge of that
    ko publish -P "github.com/knative/serving/test/test_images/$(basename ${image_dir})"
  done
}

function run_e2e_tests() {
  header "Running tests in $1"
  kubectl create namespace $2
  kubectl label namespace $2 istio-injection=enabled --overwrite
  local options=""
  (( EMIT_METRICS )) && options="-emitmetrics"
  report_go_test -v -tags=e2e -count=1 ./test/$1 ${options}

  local result=$?
  [[ ${result} -ne 0 ]] && dump_cluster_state
  return ${result}
}

# Script entry point.

initialize $@

# Fail fast during setup.
set -o errexit
set -o pipefail

header "Building and starting Knative Serving"
export KO_DOCKER_REPO=${DOCKER_REPO_OVERRIDE}
create_everything

publish_test_images

# Handle test failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

wait_until_pods_running knative-serving || fail_test "Knative Serving is not up"
wait_until_pods_running istio-system || fail_test "Istio system is not up"
wait_until_service_has_external_ip istio-system knative-ingressgateway || fail_test "Ingress has no external IP"

# Run the tests

result=0
run_e2e_tests conformance pizzaplanet || result=1
run_e2e_tests e2e noodleburg || result=1
[[ ${result} -ne 0 ]] && exit 1 # run_e2e_tests already dumps state
success
