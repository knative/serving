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

# If you already have a Knative cluster setup and kubectl pointing
# to it, call this script with the --run-tests arguments and it will use
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname $0)/e2e-common.sh

# Script entry point.

# Skip installing istio as an add-on.
# Temporarily increasing the cluster size for serving tests to rule out
# resource/eviction as causes of flakiness.
initialize --skip-istio-addon --min-nodes=4 --max-nodes=4 --enable-ha "$@"

# Run the tests
header "Running tests"

run_go_tool github.com/markusthoemmes/podspeed/cmd/podspeed@b2abf8f podspeed --prepull -pods 100 -typ knative-head
run_go_tool github.com/markusthoemmes/podspeed/cmd/podspeed@b2abf8f podspeed --prepull -pods 100 -typ knative-head-just-readiness-http
run_go_tool github.com/markusthoemmes/podspeed/cmd/podspeed@b2abf8f podspeed --prepull -pods 100 -typ knative-head-readiness-startup-http
