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

# If you already have a Kubernetes cluster setup and kubectl pointing
# to it, call this script with the --run-tests arguments and it will use
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname "$0")/e2e-common.sh

# Script entry point.
initialize --num-nodes=4 --enable-ha --cluster-version=1.31 "$@"

# Run the tests
header "Running tests"

failed=0

# Run tests serially in the mesh and https scenarios.
GO_TEST_FLAGS=()
E2E_TEST_FLAGS=()

IFS=" " read -r -a E2E_TEST_FLAGS <<< "${TEST_OPTIONS:-}"

if [[ "${E2E_TEST_FLAGS[@]}" == "" ]]; then
  E2E_TEST_FLAGS=("-resolvabledomain=$(use_resolvable_domain)" "-ingress-class=${INGRESS_CLASS}")

  # Drop testing alpha and beta features with the Gateway API
  if [[ "${INGRESS_CLASS}" != *"gateway-api"* ]]; then
    E2E_TEST_FLAGS+=("-enable-alpha" "-enable-beta")
  fi
fi

if (( HTTPS )); then
  E2E_TEST_FLAGS+=("-https")
  toggle_feature external-domain-tls Enabled config-network
  kubectl apply -f "${E2E_YAML_DIR}"/test/config/externaldomaintls/certmanager/caissuer/
  add_trap "kubectl delete -f ${E2E_YAML_DIR}/test/config/externaldomaintls/certmanager/caissuer/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
  # we need to restart the pod in order to start the net-certmanager-controller
  restart_pod "${SYSTEM_NAMESPACE}" "app=controller"
fi

if (( MESH )); then
  GO_TEST_FLAGS+=("-parallel=1")
fi

if (( SHORT )); then
  GO_TEST_FLAGS+=("-short")
fi

go_test_e2e -timeout=30m \
  "${GO_TEST_FLAGS[@]}" \
  ./test/conformance/api/... \
  ./test/conformance/runtime/... \
  ./test/e2e \
  "${E2E_TEST_FLAGS[@]}" || failed=1

toggle_feature tag-header-based-routing Enabled
go_test_e2e -timeout=2m ./test/e2e/tagheader "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature tag-header-based-routing Disabled

toggle_feature allow-zero-initial-scale true config-autoscaler || fail_test
go_test_e2e -timeout=2m ./test/e2e/initscale "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature allow-zero-initial-scale false config-autoscaler || fail_test

go_test_e2e -timeout=2m ./test/e2e/domainmapping "${E2E_TEST_FLAGS[@]}" || failed=1

toggle_feature cluster-local-domain-tls enabled config-network || fail_test
go_test_e2e -timeout=2m ./test/e2e/clusterlocaldomaintls "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature cluster-local-domain-tls disabled config-network || fail_test

toggle_feature system-internal-tls enabled config-network || fail_test
toggle_feature "logging.enable-request-log" true config-observability || fail_test
toggle_feature "logging.request-log-template" "TLS: {{.Request.TLS}}" config-observability || fail_test
# with current implementation, Activator must be restarted when configuring system-internal-tls. See https://github.com/knative/serving/issues/13754
restart_pod "${SYSTEM_NAMESPACE}" "app=activator"

# we need to restart the pod in order to start the net-certmanager-controller
if (( ! HTTPS )); then
  restart_pod "${SYSTEM_NAMESPACE}" "app=controller"
fi
go_test_e2e -timeout=3m ./test/e2e/systeminternaltls "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature system-internal-tls disabled config-network || fail_test
toggle_feature "logging.enable-request-log" false config-observability || fail_test
toggle_feature "logging.request-log-template" '' config-observability || fail_test
# with the current implementation, Activator is always in the request path, and needs to be restarted after configuring system-internal-tls
restart_pod "${SYSTEM_NAMESPACE}" "app=activator"

# we need to restart the pod to stop the net-certmanager-controller
if (( ! HTTPS )); then
  restart_pod "${SYSTEM_NAMESPACE}" "app=controller"
  kubectl get leases -n "${SYSTEM_NAMESPACE}" -o json | jq -r '.items[] | select(.metadata.name | test("controller.knative.dev.serving.pkg.reconciler.certificate.reconciler")).metadata.name' | xargs kubectl delete lease -n "${SYSTEM_NAMESPACE}"
fi

kubectl get cm "config-gc" -n "${SYSTEM_NAMESPACE}" -o yaml > "${TMP_DIR}"/config-gc.yaml
add_trap "kubectl replace cm 'config-gc' -n ${SYSTEM_NAMESPACE} -f ${TMP_DIR}/config-gc.yaml" SIGKILL SIGTERM SIGQUIT
immediate_gc
go_test_e2e -timeout=2m ./test/e2e/gc "${E2E_TEST_FLAGS[@]}" || failed=1
kubectl replace cm "config-gc" -n "${SYSTEM_NAMESPACE}" -f "${TMP_DIR}"/config-gc.yaml

# Run scale tests.
# Note that we use a very high -parallel because each ksvc is run as its own
# sub-test. If this is not larger than the maximum scale tested then the test
# simply cannot pass.
# TODO - Renable once we get this reliably passing on GKE 1.21
# go_test_e2e -timeout=20m -parallel=300 ./test/scale "${E2E_TEST_FLAGS[@]}" || failed=1

# Run HPA tests
go_test_e2e -timeout=30m -tags=hpa ./test/e2e "${E2E_TEST_FLAGS[@]}" || failed=1

# Run initContainers tests with alpha enabled avoiding any issues with the testing options guard above
# InitContainers test uses emptyDir.
toggle_feature kubernetes.podspec-init-containers Enabled
go_test_e2e -timeout=2m ./test/e2e/initcontainers "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature kubernetes.podspec-init-containers Disabled

# Run multi-container probe tests
toggle_feature multi-container-probing Enabled
go_test_e2e -timeout=2m ./test/e2e/multicontainerprobing "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature multi-container-probing Disabled

# RUN PVC tests with default storage class.
toggle_feature kubernetes.podspec-persistent-volume-claim Enabled
toggle_feature kubernetes.podspec-persistent-volume-write Enabled
toggle_feature kubernetes.podspec-securitycontext Enabled
go_test_e2e -timeout=5m ./test/e2e/pvc "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature kubernetes.podspec-securitycontext Disabled
toggle_feature kubernetes.podspec-persistent-volume-write Disabled
toggle_feature kubernetes.podspec-persistent-volume-claim Disabled

# RUN secure pod defaults test in a separate install.
toggle_feature secure-pod-defaults Enabled
go_test_e2e -timeout=3m ./test/e2e/securedefaults "${E2E_TEST_FLAGS[@]}" || failed=1
toggle_feature secure-pod-defaults Disabled

# Run HA tests separately as they're stopping core Knative Serving pods.
# Define short -spoofinterval to ensure frequent probing while stopping pods.
go_test_e2e -timeout=30m -failfast -parallel=1 ./test/ha \
  "${E2E_TEST_FLAGS[@]}" \
  -replicas="${REPLICAS:-1}" \
  -buckets="${BUCKETS:-1}" \
  -spoofinterval="10ms" || failed=1

if (( HTTPS )); then
  kubectl delete -f "${E2E_YAML_DIR}"/test/config/externaldomaintls/certmanager/caissuer/ --ignore-not-found
  toggle_feature external-domain-tls Disabled config-network
  # we need to restart the pod to stop the net-certmanager-controller
  restart_pod "${SYSTEM_NAMESPACE}" "app=controller"
  kubectl get leases -n "${SYSTEM_NAMESPACE}" -o json | jq -r '.items[] | select(.metadata.name | test("controller.knative.dev.serving.pkg.reconciler.certificate.reconciler")).metadata.name' | xargs kubectl delete lease -n "${SYSTEM_NAMESPACE}"
fi

(( failed )) && fail_test

# Remove the kail log file if the test flow passes.
# This is for preventing too many large log files to be uploaded to GCS in CI.
rm "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt"

success
