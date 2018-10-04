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

# This script runs the presubmit tests; it is started by prow for each PR.
# For convenience, it can also be executed manually.
# Running the script without parameters, or with the --all-tests
# flag, causes all tests to be executed, in the right order.
# Use the flags --build-tests, --unit-tests and --integration-tests
# to run a specific set of tests.

source $(dirname $0)/cluster.sh

# Location of istio for the test cluster
readonly PERF_DIR="${REPO_ROOT_DIR}/test/performance/observed-concurrency"
readonly TEST_APP_YAML="${PERF_DIR}/app.yaml"

function perf_tests() {
  header "Running performance tests"
  echo "Kubernetes version: $(kubectl version -o yaml | grep -A 20 serverVersion | grep gitVersion)"
  subheader "Node Capacity"
  kubectl get nodes -o=custom-columns=NAME:.metadata.name,KUBELET:.status.nodeInfo.kubeletVersion,KERNEL:.status.nodeInfo.kernelVersion,OS:.status.nodeInfo.osImage,CPUs:.status.capacity.cpu,MEMORY:.status.capacity.memory

  # We need to wait to get the hostname and ipvalues as it takes a few seconds to get the route propogated.
  sleep 1m
  local ip=$(kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}")
  local host=$(kubectl get route observed-concurrency -o jsonpath="{.status.domain}")
  
  wait_until_routable "$ip" "$host" || return 1
  wrk -t 1 -c "$1" -d "$2" -s "${PERF_DIR}/reporter.lua" --latency -H "Host: ${host}" "http://${ip}/?timeout=1000"
}

# Deletes everything created on the cluster including all knative and istio components.
function teardown() {
  # Delete the service now that the test is done
  kubectl delete --ignore-not-found=true -f ${TEST_APP_YAML}
  delete_everything
}

# Fail fast during setup.
set -o errexit
set -o pipefail

header "Setting up environment"

initialize $@
create_everything

wait_until_cluster_up
ko apply -f ${TEST_APP_YAML}

# Handle test failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

# Run the test with concurrency=5 and for 60s duration. 
# Need to export concurrency var as it is required by the parser.
export concurrency=5
perf_tests "${concurrency}" 60s

success
