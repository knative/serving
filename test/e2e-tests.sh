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

function wait_for_leader_controller() {
  echo -n "Waiting for a leader Controller"
  for i in {1..150}; do  # timeout after 5 minutes
    local leader=$(kubectl get lease controller -n "${SYSTEM_NAMESPACE}" -ojsonpath='{.spec.holderIdentity}' | cut -d"_" -f1)
    # Make sure the leader pod exists.
    if [ -n "${leader}" ] && kubectl get pod "${leader}" -n "${SYSTEM_NAMESPACE}"  >/dev/null 2>&1; then
      echo -e "\nNew leader Controller has been elected"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for leader controller"
  return 1
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
(( MESH )) && parallelism="-parallel 1"

if [[ "${ISTIO_VERSION}" == "1.5-latest" ]]; then
  parallelism="-parallel 1"
fi

if (( HTTPS )); then
  use_https="--https"
  # TODO: parallel 1 is necessary until https://github.com/knative/serving/issues/7406 is solved.
  parallelism="-parallel 1"
  turn_on_auto_tls
  kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/caissuer/
  add_trap "kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/caissuer/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
  add_trap "turn_off_auto_tls" SIGKILL SIGTERM SIGQUIT
fi

# Enable allow-zero-initial-scale before running e2e tests (for test/e2e/initial_scale_test.go)
kubectl -n ${SYSTEM_NAMESPACE} patch configmap/config-autoscaler --type=merge --patch='{"data":{"allow-zero-initial-scale":"true"}}'
add_trap "kubectl -n ${SYSTEM_NAMESPACE} patch configmap/config-autoscaler --type=merge --patch='{\"data\":{\"allow-zero-initial-scale\":\"false\"}}'" SIGKILL SIGTERM SIGQUIT

# Run conformance and e2e tests.

go_test_e2e -timeout=30m \
  $(go list ./test/conformance/... | grep -v certificate) \
  ./test/e2e ./test/e2e/hpa \
  ${parallelism} \
  "--resolvabledomain=$(use_resolvable_domain)" "${use_https}" "$(ingress_class)" || failed=1

if (( HTTPS )); then
  kubectl delete -f ${TMP_DIR}/test/config/autotls/certmanager/caissuer/ --ignore-not-found
  turn_off_auto_tls
fi


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
kubectl -n "${SYSTEM_NAMESPACE}" patch configmap/config-leader-election --type=merge \
  --patch='{"data":{"enabledComponents":"controller,hpaautoscaler"}}'
add_trap "kubectl get cm config-leader-election -n ${SYSTEM_NAMESPACE} -oyaml | sed '/.*enabledComponents.*/d' | kubectl replace -f -" SIGKILL SIGTERM SIGQUIT

# Save activator HPA original values for later use.
min_replicas=$(kubectl get hpa activator -n "${SYSTEM_NAMESPACE}" -ojsonpath='{.spec.minReplicas}')
max_replicas=$(kubectl get hpa activator -n "${SYSTEM_NAMESPACE}" -ojsonpath='{.spec.maxReplicas}')
kubectl patch hpa activator -n "${SYSTEM_NAMESPACE}" \
  --type 'merge' \
  --patch '{"spec": {"maxReplicas": '2', "minReplicas": '2'}}'
add_trap "kubectl patch hpa activator -n ${SYSTEM_NAMESPACE} \
  --type 'merge' \
  --patch '{\"spec\": {\"maxReplicas\": '$max_replicas', \"minReplicas\": '$minReplicas'}}'" SIGKILL SIGTERM SIGQUIT

for deployment in controller autoscaler-hpa; do
  # Make sure all pods run in leader-elected mode.
  kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "$deployment" --replicas=0
  # Scale up components for HA tests
  kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "$deployment" --replicas=2
done
add_trap "for deployment in controller autoscaler-hpa; do \
  kubectl -n ${SYSTEM_NAMESPACE} scale deployment $deployment --replicas=0; \
  kubectl -n ${SYSTEM_NAMESPACE} scale deployment $deployment --replicas=1; done" SIGKILL SIGTERM SIGQUIT

# Wait for a new leader Controller to prevent race conditions during service reconciliation
wait_for_leader_controller || failed=1

# Give the controller time to sync with the rest of the system components.
sleep 30

# Define short -spoofinterval to ensure frequent probing while stopping pods
go_test_e2e -timeout=15m -failfast -parallel=1 ./test/ha -spoofinterval="10ms" || failed=1

kubectl get cm config-leader-election -n "${SYSTEM_NAMESPACE}" -oyaml | sed '/.*enabledComponents.*/d' | kubectl replace -f -
for deployment in controller autoscaler-hpa; do
  kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "$deployment" --replicas=0
  kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "$deployment" --replicas=1
done
kubectl patch hpa activator -n "${SYSTEM_NAMESPACE}" \
  --type 'merge' \
  --patch '{"spec": {"maxReplicas": '$max_replicas', "minReplicas": '$min_replicas'}}'

# Dump cluster state in case of failure
(( failed )) && dump_cluster_state
(( failed )) && fail_test

success
