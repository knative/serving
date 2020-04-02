#!/bin/bash

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

# performance-tests.sh is added to manage all clusters that run the performance
# benchmarks in serving repo, it is ONLY intended to be run by Prow, users
# should NOT run it manually.

# Setup env vars to override the default settings
export BENCHMARK_ROOT_PATH="$GOPATH/src/knative.dev/serving/test/performance/benchmarks"

source vendor/knative.dev/test-infra/scripts/performance-tests.sh
source $(dirname $0)/../e2e-common.sh

function update_knative() {
  local istio_version="istio-1.4-latest"
  # Mako needs to escape '.' in tags. Use '_' instead.
  local istio_version_escaped=${istio_version//./_}

  pushd .
  cd ${GOPATH}/src/knative.dev
  echo ">> Update istio"
  # Some istio pods occasionally get overloaded and die, delete all deployments
  # and services from istio before reintalling it, to get them freshly recreated
  kubectl delete deployments --all -n istio-system
  kubectl delete services --all -n istio-system
  kubectl apply -f serving/third_party/$istio_version/istio-crds.yaml || abort "Failed to apply istio-crds"
  kubectl apply -f serving/third_party/$istio_version/istio-ci-no-mesh.yaml || abort "Failed to apply istio-ci-no-mesh"

  # Overprovision the Istio gateways and pilot.
  kubectl patch hpa -n istio-system istio-ingressgateway \
    --patch '{"spec": {"minReplicas": 10, "maxReplicas": 10}}'
  kubectl patch deploy -n istio-system cluster-local-gateway \
    --patch '{"spec": {"replicas": 10}}'

  echo ">> Updating serving"
  local SERVING_CONFIG_PATH=${TMP_DIR}/serving/config
  mkdir -p ${SERVING_CONFIG_PATH}
  cp -r serving/config/* ${SERVING_CONFIG_PATH}
  find ${SERVING_CONFIG_PATH} -type f -name "*.yaml" -exec sed -i "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${E2E_SYSTEM_NAMESPACE}/g" {} +
  # Retry installation for at most three times as there can sometime be a race condition when applying serving CRDs
  local n=0
  until [ $n -ge 3 ]
  do
    ko apply -f ${SERVING_CONFIG_PATH}/ && break
    n=$[$n+1]
  done
  if [ $n == 3 ]; then
    abort "Failed to patch serving"
  fi
  popd

  # Update the activator hpa minReplicas to 10
  kubectl patch hpa -n ${E2E_SYSTEM_NAMESPACE} activator \
    --patch '{"spec": {"minReplicas": 10}}'
  # Update the scale-to-zero grace period to 10s
  kubectl patch configmap/config-autoscaler \
    -n ${E2E_SYSTEM_NAMESPACE} \
    --type merge \
    -p '{"data":{"scale-to-zero-grace-period":"10s"}}'

  echo ">> Setting up 'prod' config-mako"
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-mako
data:
  # This should only be used by our performance automation.
  environment: prod
  repository: serving
  additionalTags: "istio=$istio_version_escaped"
  slackConfig: |
    benchmarkChannels:
      "Serving dataplane probe":
      - name: networking
        identity: CA9RHBGJX
      "Serving deployment probe":
      - name: serving-api
        identity: CA4DNJ9A4
      "Serving load testing":
      - name: autoscaling
        identity: C94SPR60H
EOF
}

function update_benchmark() {
  local TEST_BENCHMARK_ROOT_PATH=${TMP_DIR}/$1/continuous
  mkdir -p ${TEST_BENCHMARK_ROOT_PATH}
  cp -r ${BENCHMARK_ROOT_PATH}/$1/continuous/* ${TEST_BENCHMARK_ROOT_PATH}
  find ${TEST_BENCHMARK_ROOT_PATH} -type f -name "*.yaml" -exec sed -i "s/${KNATIVE_DEFAULT_NAMESPACE}/${E2E_SYSTEM_NAMESPACE}/g" {} +

  echo ">> Deleting all the yamls for benchmark $1"
  ko delete -f ${TEST_BENCHMARK_ROOT_PATH} --ignore-not-found=true
  echo ">> Deleting all Knative serving services"
  kubectl delete ksvc --all

  echo ">> Applying all the yamls for benchmark $1"
  ko apply -f ${TEST_BENCHMARK_ROOT_PATH} || abort "failed to apply benchmarks yaml $1"
}

main $@
