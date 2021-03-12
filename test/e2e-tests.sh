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

# Helper functions.

function knative_setup() {
  install_knative_serving
}

# Script entry point.

# Skip installing istio as an add-on.
# Temporarily increasing the cluster size for serving tests to rule out
# resource/eviction as causes of flakiness.
initialize "$@" --skip-istio-addon --min-nodes=4 --max-nodes=4

# Run the tests
header "Running tests"

failed=0

# Run tests serially in the mesh and https scenarios.
parallelism=""
use_https=""
if (( MESH )); then
  parallelism="-parallel 1"
fi

# Keep the bucket count in sync with test/ha/ha.go.
kubectl -n "${SYSTEM_NAMESPACE}" patch configmap/config-leader-election --type=merge \
  --patch='{"data":{"buckets": "'${BUCKETS}'"}}' || fail_test

kubectl patch hpa activator -n "${SYSTEM_NAMESPACE}" \
  --type "merge" \
  --patch '{"spec": {"minReplicas": '${REPLICAS}', "maxReplicas": '${REPLICAS}'}}' || fail_test

# Scale up all of the HA components in knative-serving.
scale_controlplane "${HA_COMPONENTS[@]}"

# Changing the bucket count and cycling the controllers will leave around stale
# lease resources at the old sharding factor, so clean these up.
kubectl -n ${SYSTEM_NAMESPACE} delete leases --all

# Wait for a new leader Controller to prevent race conditions during service reconciliation.
wait_for_leader_controller || fail_test

# Dump the leases post-setup.
header "Leaders"
kubectl get lease -n "${SYSTEM_NAMESPACE}"

# Give the controller time to sync with the rest of the system components.
sleep 30

# Run conformance and e2e tests.

# Currently only Istio, Contour and Kourier implement the alpha features.
alpha=""
if [[ -z "${INGRESS_CLASS}" \
  || "${INGRESS_CLASS}" == "istio.ingress.networking.knative.dev" \
  || "${INGRESS_CLASS}" == "contour.ingress.networking.knative.dev" \
  || "${INGRESS_CLASS}" == "kourier.ingress.networking.knative.dev" ]]; then
  alpha="-enable-alpha"
fi

if (( HTTPS )); then
  export KNATIVE_TEST_OPTIONAL_RESOURCES=autoTLS
  use_https="-https"
fi

go_test_e2e -p 1 -exec "go run knative.dev/serving/test/cmd/runner" \
  -timeout=30m \
  ./test/conformance/... \
  ./test/e2e/gc/... \
  ./test/e2e/initscale/... \
  ./test/e2e/multicontainer/... \
  ./test/e2e/tagheader/... \
  ${alpha} \
  -enable-beta \
  ${parallelism} \
  "${use_https}" \
  "-resolvabledomain=$(use_resolvable_domain)" \
  "$(ingress_class)" || failed=1

# Run scale tests.
# Note that we use a very high -parallel because each ksvc is run as its own
# sub-test. If this is not larger than the maximum scale tested then the test
# simply cannot pass.
go_test_e2e -timeout=20m -parallel=300 ./test/scale || failed=1

# Run HA tests separately as they're stopping core Knative Serving pods.
# Define short -spoofinterval to ensure frequent probing while stopping pods.
go_test_e2e -timeout=25m -failfast -parallel=1 ./test/ha \
	    -replicas="${REPLICAS:-1}" -buckets="${BUCKETS:-1}" -spoofinterval="10ms" || failed=1

(( failed )) && fail_test

# Remove the kail log file if the test flow passes.
# This is for preventing too many large log files to be uploaded to GCS in CI.
rm "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt"
success
