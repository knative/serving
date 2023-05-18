#!/usr/bin/env bash

# Copyright 2022 The Knative Authors
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

# This script runs the performance tests using mako sidecar stub against Knative
# Serving built from source. It can be optionally started for each PR.
# For convenience, it can also be executed manually.

# If you already have a Kubernetes cluster setup and kubectl pointing
# to it, call this script with the --run-tests arguments and it will use
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname $0)/../e2e-common.sh

# Skip installing istio as an add-on.
# Temporarily increasing the cluster size for serving tests to rule out
# resource/eviction as causes of flakiness.
initialize --skip-istio-addon --min-nodes=10 --max-nodes=10 --perf --cluster-version=1.25 "$@"

# Run tests serially in the mesh and https scenarios.
parallelism=""
use_https=""

function wait_for_test() {
  echo "waiting for test to complete"
  for i in {1..600}; do
      echo -n "#"
      sleep 1
  done
  echo ""
}

function run_ytt_for_test() {
      run_ytt \
      -f "$@" \
      -f "${REPO_ROOT_DIR}/test/config/ytt/performance/influx" \
      -f "${REPO_ROOT_DIR}/test/config/ytt/performance/pods" \
      --data-value dockerrepo="${KO_DOCKER_REPO}" \
      --data-value prowtag="${PROW_TAG}" \
      --data-value influxurl="${INFLUX_URL}" \
      --data-value influxtoken="${INFLUX_TOKEN}" \
      --output-files "${ARTIFACTS}/mako-overlay"
}

mkdir -p "${ARTIFACTS}/mako"
echo Results downloaded to "${ARTIFACTS}/mako"

mkdir -p "${ARTIFACTS}/mako-overlay"

export PROW_TAG="local"
if (( IS_PROW )); then
      export PROW_TAG=${PROW_JOB_ID}
      export INFLUX_URL=$(cat /etc/influx-url-secret-volume/influxdb-url)
      export INFLUX_TOKEN=$(cat /etc/influx-token-secret-volume/influxdb-token)
fi
echo ${PROW_JOB_ID}
echo ${PROW_TAG}
###############################################################################################
header "Create influx secret"

run_ytt \
      -f "${REPO_ROOT_DIR}/test/config/ytt/performance/influx" \
      --data-value influxurl="${INFLUX_URL}" \
      --data-value influxtoken="${INFLUX_TOKEN}" \
      --output-files "${ARTIFACTS}/mako-overlay"

kubectl apply -f "${ARTIFACTS}/mako-overlay/influx-secret.yaml"

###############################################################################################
header "Dataplane probe performance test"
kubectl delete job  dataplane-probe-deployment dataplane-probe-istio dataplane-probe-queue dataplane-probe-activator --ignore-not-found=true
kubectl delete configmap config-mako -n default --ignore-not-found=true

kubectl create configmap config-mako -n default --from-file=test/performance/benchmarks/dataplane-probe/dev.config

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe-direct.yaml"

ko apply -f test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe-setup.yaml
ko apply -f "${ARTIFACTS}/mako-overlay/dataplane-probe-direct.yaml"

wait_for_test

############################################################################################
header "Deployment probe performance test"
kubectl delete job  deployment-probe --ignore-not-found=true
kubectl delete configmap config-mako -n default --ignore-not-found=true
kubectl create configmap config-mako -n default --from-file=test/performance/benchmarks/deployment-probe/dev.config

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/deployment-probe/continuous/benchmark-direct.yaml"

ko apply -f "${ARTIFACTS}/mako-overlay/benchmark-direct.yaml"

wait_for_test

###############################################################################################
header "Scale from Zero performance test"

kubectl delete job scale-from-zero-1 scale-from-zero-5 scale-from-zero-25 --ignore-not-found=true
kubectl delete configmap config-mako -n default --ignore-not-found=true

kubectl create configmap config-mako -n default --from-file=test/performance/benchmarks/scale-from-zero/dev.config

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/scale-from-zero/continuous/scale-from-zero-direct.yaml"

echo ">> Upload the test images"
# Upload helloworld test image as it's used by the scale-from-zero benchmark.
ko resolve -RBf test/test_images/helloworld > /dev/null

ko apply -f "${ARTIFACTS}/mako-overlay/scale-from-zero-direct.yaml"

wait_for_test

###############################################################################################
header "Load test"
kubectl delete configmap config-mako -n default --ignore-not-found=true

kubectl create configmap config-mako -n default --from-file=test/performance/benchmarks/load-test/dev.config

ko apply -f test/performance/benchmarks/load-test/continuous/load-test-setup.yaml

###############################################################################################
header "Load test zero"
kubectl delete job load-test-zero --ignore-not-found=true

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/load-test/continuous/load-test-0-direct.yaml"

ko apply -f "${ARTIFACTS}/mako-overlay/load-test-0-direct.yaml"

wait_for_test

# clean up for the next test
kubectl delete job load-test-zero --ignore-not-found=true
kubectl delete ksvc load-test-zero  --ignore-not-found=true
echo "waiting for cleanup to complete"
for i in {1..60}; do
        echo -n "#"
        sleep 1
done
echo ""

###############################################################################################
header "Load test always"
kubectl delete job load-test-always --ignore-not-found=true

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/load-test/continuous/load-test-always-direct.yaml"

ko apply -f "${ARTIFACTS}/mako-overlay/load-test-always-direct.yaml"

wait_for_test

# clean up for the next test
kubectl delete job load-test-always --ignore-not-found=true
kubectl delete ksvc load-test-always --ignore-not-found=true
echo "waiting for cleanup to complete"
for i in {1..60}; do
      echo -n "#"
      sleep 1
done
echo ""

#############################################################################################
header "Load test 200"
kubectl delete job load-test-200 --ignore-not-found=true

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/load-test/continuous/load-test-200-direct.yaml"

ko apply -f "${ARTIFACTS}/mako-overlay/load-test-200-direct.yaml"

wait_for_test

# clean up for the next test
kubectl delete job load-test-200 --ignore-not-found=true
kubectl delete ksvc load-test-200  --ignore-not-found=true
echo "waiting for cleanup to complete"
for i in {1..60}; do
      echo -n "#"
      sleep 1
done
echo ""

##############################################################################################
header "Rollout probe performance test"
kubectl delete configmap config-mako -n default --ignore-not-found=true

kubectl create configmap config-mako -n default --from-file=test/performance/benchmarks/rollout-probe/dev.config

ko apply -f test/performance/benchmarks/rollout-probe/continuous/rollout-probe-setup.yaml

################################################################################################
header "Rollout probe performance test with activator"
kubectl delete job rollout-probe-activator-with-cc --ignore-not-found=true

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/continuous/rollout-probe-activator-direct.yaml"

wait_for_test
echo ""

##############################################################################################
header "Rollout probe performance test with activator lin"
kubectl delete job rollout-probe-activator-with-cc-lin --ignore-not-found=true

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/continuous/rollout-probe-activator-lin-direct.yaml"

wait_for_test
echo ""

###############################################################################################
header "Rollout probe performance test with queue"
kubectl delete job rollout-probe-queue-with-cc --ignore-not-found=true

run_ytt_for_test "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/continuous/rollout-probe-queue-direct.yaml"

wait_for_test
echo ""

success
