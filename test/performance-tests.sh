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

# This script runs the presubmit tests; it is started by prow for each PR.
# For convenience, it can also be executed manually.
# Running the script without parameters, or with the --all-tests
# flag, causes all tests to be executed, in the right order.
# Use the flags --build-tests, --unit-tests and --integration-tests
# to run a specific set of tests.

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/e2e-tests.sh

# Location of istio for the test cluster
readonly ISTIO_YAML=./third_party/istio-1.0.1/istio-lean.yaml

function create_istio() {
  echo ">> Bringing up Istio"
  kubectl apply -f ${ISTIO_YAML}
}

function create_everything() {
  create_istio
  echo ">> Bringing up Serving"
  kubectl apply -f third_party/config/build/release.yaml
  ko apply -f config/
  # Due to the lack of Status in Istio, we have to ignore failures in initial requests.
  #
  # However, since network configurations may reach different ingress pods at slightly
  # different time, even ignoring failures for initial requests won't ensure subsequent
  # requests will succeed all the time.  We are disabling ingress pod autoscaling here
  # to avoid having too much flakes in the tests.  That would allow us to be stricter
  # when checking non-probe requests to discover other routing issues.
  #
  # We should revisit this when Istio API exposes a Status that we can rely on.
  # TODO(tcnghia): remove this when https://github.com/istio/istio/issues/882 is fixed.
  echo ">> Patching Istio"
  kubectl patch hpa -n istio-system knative-ingressgateway --patch '{"spec": {"maxReplicas": 1}}'  
}

function perf_tests() {
  header "Running performance tests"
  echo "Kubernetes version: $(kubectl version -o yaml | grep -A 20 serverVersion | grep gitVersion)"
  subheader "Node Capacity"
  kubectl get nodes -o=custom-columns=NAME:.metadata.name,KUBELET:.status.nodeInfo.kubeletVersion,KERNEL:.status.nodeInfo.kernelVersion,OS:.status.nodeInfo.osImage,CPUs:.status.capacity.cpu,MEMORY:.status.capacity.memory

  # We need to wait to get the hostname and ipvalues as it takes a few seconds to get the route propogated.
  sleep 1m
  local ip=$(kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}")
  local host=$(kubectl get route observed-concurrency -o jsonpath="{.status.domain}")
  
  wait_until_routable "$ip" "$host"
  wrk -t 1 -c "$1" -d "$2" -s "${REPO_ROOT_DIR}/test/performance/observed-concurrency/reporter.lua" --latency -H "Host: $host" "http://$ip/?timeout=1000"
}

function delete_istio() {
  echo ">> Bringing down Istio"
  kubectl delete --ignore-not-found=true -f ${ISTIO_YAML}
  kubectl delete clusterrolebinding cluster-admin-binding
}

function delete_everything() {
  echo ">> Bringing down Serving"
  ko delete --ignore-not-found=true -f config/
  kubectl delete --ignore-not-found=true -f third_party/config/build/release.yaml
  delete_istio
}

header "Setting up environment"

# Fail fast during setup.
set -o errexit
set -o pipefail

initialize $@
create_everything

wait_until_pods_running knative-serving || fail_test "Knative Serving is not up"
wait_until_pods_running istio-system || fail_test "Istio system is not up"
wait_until_service_has_external_ip istio-system knative-ingressgateway || fail_test "Ingress has no external IP"

ko apply -f "${REPO_ROOT_DIR}/test/performance/observed-concurrency/app.yaml"

# Run the test with concurrency=5 and for 60s duration. 
# Need to export concurrency var as it is required by the parser.
export concurrency=5
perf_tests "$concurrency" 60s

# Delete the service now that the test is done
kubectl delete -f "${REPO_ROOT_DIR}/test/performance/observed-concurrency/app.yaml"

delete_everything