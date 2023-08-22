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

# This script runs the performance tests against Knative
# Serving built from source. It can be optionally started for each PR.
# For convenience, it can also be executed manually.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname $0)/../e2e-common.sh

set -o errexit
set -o nounset
set -o pipefail

declare PROW_TAG
declare PROW_JOB_ID

ns="default"

# Skip installing istio as an add-on.
# Temporarily increasing the cluster size for serving tests to rule out
# resource/eviction as causes of flakiness.
initialize --num-nodes=10 --perf --cluster-version=1.25 "$@"

function scale_activator() {
  local replicas=$1

  echo "Setting activator replicas to ${replicas}"
  kubectl -n knative-serving patch hpa activator --patch "{\"spec\":{\"minReplicas\": ${replicas}, \"maxReplicas\": ${replicas} }}"

  # Wait for HPA to do the scaling
  sleep 30
}

function run_job() {
  local name=$1
  local file=$2

  # cleanup from old runs
  kubectl delete job "$name" -n "$ns" --ignore-not-found=true

  # start the load test and get the logs
  envsubst < "$file" | ko apply --sbom=none -f -

  # Follow logs to wait for job termination
  kubectl wait --for=condition=ready -n "$ns" pod --selector=job-name="$name" --timeout=-1s
  kubectl logs -n "$ns" -f "job.batch/$name"

  # Dump logs to a file to upload it as CI job artifact
  kubectl logs -n "$ns" "job.batch/$name" >"$name.log"

  # clean up
  kubectl delete "job/$name" -n "$ns" --ignore-not-found=true
  kubectl wait --for=delete "job/$name" --timeout=60s -n "$ns"
}

export PROW_TAG="local"
if ((IS_PROW)); then
  export PROW_TAG=${PROW_JOB_ID}
  export INFLUX_URL=$(cat /etc/influx-url-secret-volume/influxdb-url)
  export INFLUX_TOKEN=$(cat /etc/influx-token-secret-volume/influxdb-token)
fi

if [[ -z "${INFLUX_URL}" ]]; then
  echo "env variable 'INFLUX_URL' not specified!"
  exit 1
fi
if [[ -z "${INFLUX_TOKEN}" ]]; then
  echo "env variable 'INFLUX_TOKEN' not specified!"
  exit 1
fi

echo "Running load test with PROW_TAG: ${PROW_TAG}, reporting results to: ${INFLUX_URL}"

###############################################################################################
#header "Preparing cluster config"
#
kubectl delete secret performance-test-config -n "$ns" --ignore-not-found=true
kubectl create secret generic performance-test-config -n "$ns" \
  --from-literal=influxurl="${INFLUX_URL}" \
  --from-literal=influxtoken="${INFLUX_TOKEN}" \
  --from-literal=prowtag="${PROW_TAG}"

# Tweak configuration for performance tests
scale_activator 10
toggle_feature rolloutDuration 240 config-network
toggle_feature scale-to-zero-grace-period 10s config-autoscaler

echo ">> Upload the test images"
ko resolve --sbom=none -RBf test/test_images/autoscale > /dev/null
ko resolve --sbom=none -RBf test/test_images/helloworld > /dev/null

###############################################################################################
header "Dataplane probe: Setup"

ko apply -f "${REPO_ROOT_DIR}/test/performance/benchmarks/dataplane-probe/dataplane-probe-setup.yaml"
kubectl wait --timeout=60s --for=condition=ready ksvc -n "$ns" --all
kubectl wait --timeout=60s --for=condition=available deploy -n "$ns" deployment

############################################################################################
header "Dataplane probe: deployment"

run_job dataplane-probe-deployment "${REPO_ROOT_DIR}/test/performance/benchmarks/dataplane-probe/dataplane-probe-deployment.yaml"

# additional clean up
kubectl delete deploy deployment -n "$ns" --ignore-not-found=true
kubectl delete svc deployment -n "$ns" --ignore-not-found=true
kubectl wait --for=delete deploy/deployment --timeout=60s -n "$ns"
kubectl wait --for=delete svc/deployment --timeout=60s -n "$ns"
#
#############################################################################################
header "Dataplane probe: activator"

run_job dataplane-probe-activator "${REPO_ROOT_DIR}/test/performance/benchmarks/dataplane-probe/dataplane-probe-activator.yaml"

# additional clean up
kubectl delete ksvc activator -n "$ns" --ignore-not-found=true
kubectl wait --for=delete ksvc/activator --timeout=60s -n "$ns"

############################################################################################
header "Dataplane probe: queue proxy"

run_job dataplane-probe-queue "${REPO_ROOT_DIR}/test/performance/benchmarks/dataplane-probe/dataplane-probe-queue.yaml"

# additional clean up
kubectl delete ksvc queue-proxy -n "$ns" --ignore-not-found=true
kubectl wait --for=delete ksvc/queue-proxy --timeout=60s -n "$ns"

############################################################################################
header "Reconciliation delay test"

run_job reconciliation-delay "${REPO_ROOT_DIR}/test/performance/benchmarks/reconciliation-delay/reconciliation-delay.yaml"

###############################################################################################
header "Scale from Zero test"

run_job scale-from-zero-1 "${REPO_ROOT_DIR}/test/performance/benchmarks/scale-from-zero/scale-from-zero-1.yaml"
kubectl delete ksvc -n "$ns" --all --wait --now
sleep 5 # wait a bit for the cleanup to be done

run_job scale-from-zero-5 "${REPO_ROOT_DIR}/test/performance/benchmarks/scale-from-zero/scale-from-zero-5.yaml"
kubectl delete ksvc -n "$ns" --all --wait --now
sleep 25 # wait a bit for the cleanup to be done

run_job scale-from-zero-25 "${REPO_ROOT_DIR}/test/performance/benchmarks/scale-from-zero/scale-from-zero-25.yaml"
kubectl delete ksvc -n "$ns" --all --wait --now
sleep 50 # wait a bit for the cleanup to be done

run_job scale-from-zero-100 "${REPO_ROOT_DIR}/test/performance/benchmarks/scale-from-zero/scale-from-zero-100.yaml"
kubectl delete ksvc -n "$ns" --all --wait --now
sleep 100 # wait a bit for the cleanup to be done

################################################################################################
header "Load test: Setup"

ko apply -f "${REPO_ROOT_DIR}/test/performance/benchmarks/load-test/load-test-setup.yaml"
kubectl wait --timeout=60s --for=condition=ready ksvc -n "$ns" --all

################################################################################################
header "Load test: zero"

run_job load-test-zero "${REPO_ROOT_DIR}/test/performance/benchmarks/load-test/load-test-0-direct.yaml"

# additional clean up
kubectl delete ksvc load-test-zero -n "$ns"  --ignore-not-found=true
kubectl wait --for=delete ksvc/load-test-zero --timeout=60s -n "$ns"

#################################################################################################
header "Load test: always direct"

run_job load-test-always "${REPO_ROOT_DIR}/test/performance/benchmarks/load-test/load-test-always-direct.yaml"

# additional clean up
kubectl delete ksvc load-test-always -n "$ns"  --ignore-not-found=true
kubectl wait --for=delete ksvc/load-test-always --timeout=60s -n "$ns"

###############################################################################################
header "Load test: 200 direct"

run_job load-test-200 "${REPO_ROOT_DIR}/test/performance/benchmarks/load-test/load-test-200-direct.yaml"

# additional clean up
kubectl delete ksvc load-test-200 -n "$ns"  --ignore-not-found=true
kubectl wait --for=delete ksvc/load-test-200 --timeout=60s -n "$ns"

###############################################################################################
header "Rollout probe: activator direct"

ko apply -f "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/rollout-probe-setup-activator-direct.yaml"
kubectl wait --timeout=800s --for=condition=ready ksvc -n "$ns" --all

run_job rollout-probe-activator-direct "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/rollout-probe-activator-direct.yaml"

# additional clean up
kubectl delete ksvc activator-with-cc -n "$ns"  --ignore-not-found=true
kubectl wait --for=delete ksvc/activator-with-cc --timeout=60s -n "$ns"

################################################################################################
header "Rollout probe: activator direct lin"

ko apply -f "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/rollout-probe-setup-activator-direct-lin.yaml"
kubectl wait --timeout=800s --for=condition=ready ksvc -n "$ns" --all

run_job rollout-probe-activator-direct-lin "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/rollout-probe-activator-direct-lin.yaml"

# additional clean up
kubectl delete ksvc activator-with-cc-lin -n "$ns"  --ignore-not-found=true
kubectl wait --for=delete ksvc/activator-with-cc-lin --timeout=60s -n "$ns"

#################################################################################################
header "Rollout probe: queue-proxy direct"

ko apply -f "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/rollout-probe-setup-queue-proxy-direct.yaml"
kubectl wait --timeout=800s --for=condition=ready ksvc -n "$ns" --all

run_job rollout-probe-queue-proxy-direct "${REPO_ROOT_DIR}/test/performance/benchmarks/rollout-probe/rollout-probe-queue-proxy-direct.yaml"

# additional clean up
kubectl delete ksvc queue-proxy-with-cc -n "$ns"  --ignore-not-found=true
kubectl wait --for=delete ksvc/queue-proxy-with-cc --timeout=60s -n "$ns"

success
