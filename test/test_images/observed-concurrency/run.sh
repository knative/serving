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

echo "CLUSTER INFORMATION:"
echo

echo "KUBERNETES VERSION: $(kubectl version -o yaml | grep -A 20 serverVersion | grep gitVersion)"
echo
echo "NODE CAPACITY"
kubectl get nodes -o=custom-columns=NAME:.metadata.name,KUBELET:.status.nodeInfo.kubeletVersion,KERNEL:.status.nodeInfo.kernelVersion,OS:.status.nodeInfo.osImage,CPUs:.status.capacity.cpu,MEMORY:.status.capacity.memory

ip=$(kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}")
host=$(kubectl get route observed-concurrency -o jsonpath="{.status.domain}")

echo
echo "RUNNING TEST..."
echo

# exported because wrk's lua script will use it to determine the target concurrency
# unfortunately, the "connections" parameter passed to wrk is not available in the
# script.
export concurrency=${1:-5}
duration=${2:-60s}

wrk -t 1 -c "$concurrency" -d "$duration" -s "reporter.lua" --latency -H "Host: $host" "http://$ip/?timeout=1000"