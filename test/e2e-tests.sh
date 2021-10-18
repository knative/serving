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
# Pin to 1.20 since scale test is super flakey on 1.21
initialize --skip-istio-addon --min-nodes=4 --max-nodes=4 --enable-ha --cluster-version=1.20 "$@"

# Run the tests
header "Running tests"

failed=0

# Run tests serially in the mesh and https scenarios.
parallelism=""
use_https=""
if (( MESH )); then
  parallelism="-parallel 1"
fi

if (( HTTPS )); then
  use_https="--https"
  toggle_feature autoTLS Enabled config-network
  kubectl apply -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/caissuer/
  add_trap "kubectl delete -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/caissuer/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
fi

# Run conformance and e2e tests.

# Currently only Istio, Contour and Kourier implement the alpha features.
alpha=""
if [[ -z "${INGRESS_CLASS}" \
  || "${INGRESS_CLASS}" == "istio.ingress.networking.knative.dev" \
  || "${INGRESS_CLASS}" == "contour.ingress.networking.knative.dev" \
  || "${INGRESS_CLASS}" == "kourier.ingress.networking.knative.dev" ]]; then
  alpha="--enable-alpha"
fi

TEST_OPTIONS="${TEST_OPTIONS:-${alpha} --enable-beta --resolvabledomain=$(use_resolvable_domain) ${use_https}}"
if (( SHORT )); then
  TEST_OPTIONS+=" -short"
fi

toggle_feature autocreateClusterDomainClaims true config-network || fail_test
go_test_e2e -timeout=30m \
  ./test/conformance/api/... \
  ./test/conformance/runtime/... \
  ./test/e2e \
  ${parallelism} \
  ${TEST_OPTIONS} || failed=1
toggle_feature autocreateClusterDomainClaims false config-network || fail_test

toggle_feature tag-header-based-routing Enabled
go_test_e2e -timeout=2m ./test/e2e/tagheader ${TEST_OPTIONS} || failed=1
toggle_feature tag-header-based-routing Disabled

toggle_feature allow-zero-initial-scale true config-autoscaler || fail_test
go_test_e2e -timeout=2m ./test/e2e/initscale ${TEST_OPTIONS} || failed=1
toggle_feature allow-zero-initial-scale false config-autoscaler || fail_test

toggle_feature autocreateClusterDomainClaims true config-network || fail_test
go_test_e2e -timeout=2m ./test/e2e/domainmapping ${TEST_OPTIONS} || failed=1
toggle_feature autocreateClusterDomainClaims false config-network || fail_test

kubectl get cm "config-gc" -n "${SYSTEM_NAMESPACE}" -o yaml > ${TMP_DIR}/config-gc.yaml
add_trap "kubectl replace cm 'config-gc' -n ${SYSTEM_NAMESPACE} -f ${TMP_DIR}/config-gc.yaml" SIGKILL SIGTERM SIGQUIT
immediate_gc
go_test_e2e -timeout=2m ./test/e2e/gc ${TEST_OPTIONS} || failed=1
kubectl replace cm "config-gc" -n ${SYSTEM_NAMESPACE} -f ${TMP_DIR}/config-gc.yaml

# Run scale tests.
# Note that we use a very high -parallel because each ksvc is run as its own
# sub-test. If this is not larger than the maximum scale tested then the test
# simply cannot pass.
go_test_e2e -timeout=20m -parallel=300 ./test/scale ${TEST_OPTIONS} || failed=1

# Run HPA tests
go_test_e2e -timeout=30m -tags=hpa ./test/e2e ${TEST_OPTIONS} || failed=1

# Run emptyDir, initContainers tests with alpha enabled avoiding any issues with the testing options guard above
# InitContainers test uses emptyDir.
toggle_feature kubernetes.podspec-volumes-emptydir Enabled
toggle_feature kubernetes.podspec-init-containers Enabled
go_test_e2e -timeout=2m ./test/e2e/emptydir ${TEST_OPTIONS} || failed=1
go_test_e2e -timeout=2m ./test/e2e/initcontainers ${TEST_OPTIONS} || failed=1
toggle_feature kubernetes.podspec-init-containers Disabled
toggle_feature kubernetes.podspec-volumes-emptydir Disabled

# Run HA tests separately as they're stopping core Knative Serving pods.
# Define short -spoofinterval to ensure frequent probing while stopping pods.
toggle_feature autocreateClusterDomainClaims true config-network || fail_test
go_test_e2e -timeout=25m -failfast -parallel=1 ./test/ha \
  ${TEST_OPTIONS} \
  -replicas="${REPLICAS:-1}" \
  -buckets="${BUCKETS:-1}" \
  -spoofinterval="10ms" || failed=1
toggle_feature autocreateClusterDomainClaims false config-network || fail_test

if (( HTTPS )); then
  kubectl delete -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/caissuer/ --ignore-not-found
  toggle_feature autoTLS Disabled config-network
fi

(( failed )) && fail_test

# Remove the kail log file if the test flow passes.
# This is for preventing too many large log files to be uploaded to GCS in CI.
rm "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt"

header "Collecting performance data"

cat <<EOF | ko apply -f -
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: podspeed
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/minScale: "1"
    spec:
      containers:
      - image: ko://knative.dev/serving/test/test_images/helloworld
EOF

kubectl wait ksvc/podspeed --for=condition=Ready

template=$(mktemp)
kubectl get pods -lserving.knative.dev/service=podspeed -ojson | jq '.items[0]' > "$template"

run_go_tool github.com/markusthoemmes/podspeed/cmd/podspeed@358209f podspeed --prepull -pods 100 -template "$template" > "${ARTIFACTS}/pod-bringup-performance.txt"
cat "${ARTIFACTS}/pod-bringup-performance.txt"

kubectl delete ksvc podspeed

success
