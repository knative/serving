#!/usr/bin/env bash

# Copyright 2019 The Knative Authors
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

function knative_setup() {
  # Build serving, create $SERVING_YAML
  build_knative_from_source
  start_knative_serving "${SERVING_YAML}"
  start_knative_monitoring "${MONITORING_YAML}"
}

# Script entry point.

initialize $@

# Ensure Knative Serving can be uninstalled/reinstalled cleanly
subheader "Uninstalling Knative Serving"
kubectl delete --ignore-not-found=true -f ${SERVING_YAML} || fail_test
wait_until_object_does_not_exist namespaces knative-serving || fail_test
kubectl delete --ignore-not-found=true -f ${MONITORING_YAML} || fail_test
wait_until_object_does_not_exist namespaces knative-monitoring || fail_test
# Specially wait for zipkin to be deleted, as we have them installed in istio-system namespace, see
# https://github.com/knative/serving/blob/4202efc0dc12052edc0630515b101cbf8068a609/config/monitoring/tracing/zipkin/100-zipkin.yaml#L19
wait_until_object_does_not_exist service zipkin istio-system
wait_until_object_does_not_exist deployment zipkin istio-system

subheader "Reinstalling Knative Serving"
start_knative_serving "${SERVING_YAML}" || fail_test
subheader "Reinstalling Knative Monitoring"
start_knative_monitoring "${MONITORING_YAML}" || fail_test

# Run smoke test
subheader "Running smoke test"
go_test_e2e ./test/e2e -run HelloWorld || fail_test

success
