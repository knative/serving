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

# Skip installing istio as an add-on
initialize $@ --skip-istio-addon

# Run the tests
header "Running tests"

failed=0

# Run tests serially in the mesh and https scenarios
parallelism=""
use_https=""
if (( MESH )); then
  parallelism="-parallel 1"
  # This is a workaround until Istio fixes https://github.com/istio/istio/issues/23485.
  kubectl patch mutatingwebhookconfigurations istio-sidecar-injector -p '{"webhooks": [{"name": "sidecar-injector.istio.io", "sideEffects": "None"}]}'
fi

if (( HTTPS )); then
  use_https="--https"
  # TODO: parallel 1 is necessary until https://github.com/knative/serving/issues/7406 is solved.
  parallelism="-parallel 1"
  toggle_feature autoTLS Enabled config-network
  kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/caissuer/
  add_trap "kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/caissuer/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
fi

# Enable allow-zero-initial-scale before running e2e tests (for test/e2e/initial_scale_test.go)
kubectl -n ${SYSTEM_NAMESPACE} patch configmap/config-autoscaler --type=merge --patch='{"data":{"allow-zero-initial-scale":"true"}}' || fail_test

# Keep the bucket count in sync with test/ha/ha.go
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

# Wait for a new leader Controller to prevent race conditions during service reconciliation
wait_for_leader_controller || fail_test

# Dump the leases post-setup.
header "Leaders"
kubectl get lease -n "${SYSTEM_NAMESPACE}"

# Give the controller time to sync with the rest of the system components.
sleep 30

# Run conformance and e2e tests.

go_test_e2e -timeout=30m \
  ./test/conformance/api/... ./test/conformance/runtime/... \
  ./test/e2e \
  ${parallelism} \
  "--resolvabledomain=$(use_resolvable_domain)" "${use_https}" "$(ingress_class)" || failed=1

# We run KIngress conformance ingress separately, to make it easier to skip some tests.
go_test_e2e -timeout=20m ./test/conformance/ingress ${parallelism}  \
  `# Skip TestUpdate due to excessive flaking https://github.com/knative/serving/issues/8032` \
  -run="TestIngressConformance/^[^u]" \
  "--resolvabledomain=$(use_resolvable_domain)" "${use_https}" "$(ingress_class)" || failed=1

if (( HTTPS )); then
  kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/caissuer/ --ignore-not-found
  toggle_feature autoTLS Disabled config-network
fi

toggle_feature tagHeaderBasedRouting Enabled config-network
go_test_e2e -timeout=2m ./test/e2e/tagheader || failed=1
toggle_feature tagHeaderBasedRouting Disabled config-network

toggle_feature multi-container Enabled
go_test_e2e -timeout=2m ./test/e2e/multicontainer || failed=1
toggle_feature multi-container Disabled

toggle_feature responsive-revision-gc Enabled
go_test_e2e -timeout=2m ./test/e2e/gc || failed=1
toggle_feature responsive-revision-gc Disabled

# Certificate conformance tests must be run separately
# because they need cert-manager specific configurations.
kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/selfsigned/
add_trap "kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/selfsigned/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m ./test/conformance/certificate/nonhttp01 "$(certificate_class)" || failed=1
kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/selfsigned/

kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/http01/
add_trap "kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/http01/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m ./test/conformance/certificate/http01 "$(certificate_class)" || failed=1
kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/http01/

# Run scale tests.
go_test_e2e -timeout=10m ${parallelism} ./test/scale || failed=1

# Istio E2E tests mutate the cluster and must be ran separately
if [[ -n "${ISTIO_VERSION}" ]]; then
  kubectl apply -f ${TMP_DIR}/test/config/security/authorization_ingress.yaml || return 1
  add_trap "kubectl delete -f ${TMP_DIR}/test/config/security/authorization_ingress.yaml --ignore-not-found" SIGKILL SIGTERM SIGQUIT
  go_test_e2e -timeout=10m ./test/e2e/istio "--resolvabledomain=$(use_resolvable_domain)" || failed=1
  kubectl delete -f ${TMP_DIR}/test/config/security/authorization_ingress.yaml
fi

# Run HA tests separately as they're stopping core Knative Serving pods
# Define short -spoofinterval to ensure frequent probing while stopping pods
go_test_e2e -timeout=15m -failfast -parallel=1 ./test/ha \
	    -replicas="${REPLICAS:-1}" -buckets="${BUCKETS:-1}" -spoofinterval="10ms" || failed=1

(( failed )) && fail_test

success
