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

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/presubmit-tests.sh

function wait_until_routable() {
    for i in {1..150}; do  # timeout after 5 minutes
        local val=$(curl -H "Host: $2" "http://$1")
        if [[ -z "$val" ]]; then
            echo -n "."
            sleep 2
        else
            echo "App is now routable"
            return 0
        fi                
    done
    echo "Timed out waiting for app to be routable"
    return 1
}

function perf_tests() {
    header "Running performance tests"
    echo "Kubernetes version: $(kubectl version -o yaml | grep -A 20 serverVersion | grep gitVersion)"
    subheader "Node Capacity"
    kubectl get nodes -o=custom-columns=NAME:.metadata.name,KUBELET:.status.nodeInfo.kubeletVersion,KERNEL:.status.nodeInfo.kernelVersion,OS:.status.nodeInfo.osImage,CPUs:.status.capacity.cpu,MEMORY:.status.capacity.memory

    ip=$(kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}")
    host=$(kubectl get route observed-concurrency -o jsonpath="{.status.domain}")

    wait_until_routable "$ip" "$host" || fail_test "App is not routable"
    wrk -t 1 -c "$1" -d "$2" -s "reporter.lua" --latency -H "Host: $host" "http://$ip/?timeout=1000"
}

cd "${REPO_ROOT_DIR}/test/performance/observed-concurrency"
ko apply -f app.yaml
wait_until_pods_running knative-serving || fail_test "Knative Serving is not up"
wait_until_pods_running istio-system || fail_test "Istio system is not up"
wait_until_service_has_external_ip istio-system knative-ingressgateway || fail_test "Ingress has no external IP"

# Run the test with concurrency=5 and for 60s duration. 
# Need to export concurrency var as it is required by the parser.
export concurrency=5
perf_tests "$concurrency" 60s

# Delete the cluster now that the test is done
kubectl delete -f app.yaml