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
source $(dirname $0)/../e2e-networking-library.sh

# Env vars required for installing Istio.
# Install Istio no-mesh.
export MESH=0
export KNATIVE_DEFAULT_NAMESPACE="knative-serving"
export SYSTEM_NAMESPACE="knative-serving"
export ISTIO_VERSION="stable"
# Pin net-istio to a commit when the stable Istio version was 1.4.6
# TODO(chizhg): unpin the version after https://github.com/knative/serving/issues/9673 is root caused and fixed
export NET_ISTIO_COMMIT="f64ed34d3776a444372483dddc15a330c6c1ac53"
export UNINSTALL_LIST=()
export TMP_DIR=$(mktemp -d -t ci-$(date +%Y-%m-%d-%H-%M-%S)-XXXXXXXXXX)

function update_knative() {
  # Mako needs to escape '.' in tags. Use '_' instead.
  local istio_version_escaped=${ISTIO_VERSION//./_}

  echo ">> Update istio"
  # Some istio pods occasionally get overloaded and die, delete all deployments
  # and services from istio before reinstalling it, to get them freshly recreated
  kubectl delete deployments --all -n istio-system
  kubectl delete services --all -n istio-system

  # Create the ${SYSTEM_NAMESPACE} is it does not exist.
  kubectl get ns "${SYSTEM_NAMESPACE}" || kubectl create namespace "${SYSTEM_NAMESPACE}"
  install_istio "./third_party/net-istio.yaml" || abort "Failed to install Istio"

  # Overprovision the Istio gateways and pilot.
  kubectl patch hpa -n istio-system istio-ingressgateway \
    --patch '{"spec": {"minReplicas": 15, "maxReplicas": 15}}'
  kubectl patch deploy -n istio-system cluster-local-gateway \
    --patch '{"spec": {"replicas": 15}}'

  echo ">> Updating serving"
  # Retry installation for at most three times as there can sometime be a race condition when applying serving CRDs
  local n=0
  until [ $n -ge 3 ]
  do
    ko apply -Rf config/core/ && break
    n=$[$n+1]
  done
  if [ $n == 3 ]; then
    abort "Failed to patch serving"
  fi

  # Update the activator hpa minReplicas to 10
  kubectl patch hpa -n knative-serving activator \
    --patch '{"spec": {"minReplicas": 10}}'
  # Update the scale-to-zero grace period to 10s
  kubectl patch configmap/config-autoscaler \
    -n knative-serving \
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
  echo ">> Deleting all the yamls for benchmark $1"
  ko delete -f ${BENCHMARK_ROOT_PATH}/$1/continuous --ignore-not-found=true
  echo ">> Deleting all Knative serving services"
  kubectl delete ksvc --all

  echo ">> Upload the test images"
  # Upload helloworld test image as it's used by the scale-from-zero benchmark.
  ko resolve -RBf test/test_images/helloworld > /dev/null
  echo ">> Applying all the yamls for benchmark $1"
  ko apply -f ${BENCHMARK_ROOT_PATH}/$1/continuous || abort "failed to apply benchmarks yaml $1"
}

main $@
