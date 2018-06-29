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

DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

concurrency=${1:-5}
duration=${2:-60s}

echo "CLUSTER INFORMATION:"
echo

source "$DIR/../cluster_information.sh"

ip=$(kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}")
host=$(kubectl get route observed-concurrency -o jsonpath="{.status.domain}")

echo
echo "RUNNING TEST..."
echo

wrk -t 1 -c "$concurrency" -d "$duration" -s "$DIR/reporter.lua" --latency -H "Host: $host" "http://$ip/?timeout=1000"

echo
echo "GENERATING REPORT..."
echo

python "$DIR/reporting.py" "$concurrency"