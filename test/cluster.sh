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

# This script provides helper methods to perform cluster actions.

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/e2e-tests.sh

# Location of istio for the test cluster
readonly ISTIO_YAML=./third_party/istio-1.0.2/istio-lean.yaml

function create_istio() {
  echo ">> Bringing up Istio"
  kubectl apply -f ${ISTIO_YAML}
}

function create_monitoring() {
  echo ">> Bringing up Monitoring"
  kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
}

function create_everything() {
  create_istio
  echo ">> Bringing up Serving"
  kubectl apply -f third_party/config/build/release.yaml
  ko apply -f config/
  # Due to the lack of Status in Istio, we have to ignore failures in initial requests.
  #
  # However, since network configurations may reach different ingress pods at slightly
  # different time, even ignoring failures for initial requests won't ensure subsequent
  # requests will succeed all the time.  We are disabling ingress pod autoscaling here
  # to avoid having too much flakes in the tests.  That would allow us to be stricter
  # when checking non-probe requests to discover other routing issues.
  #
  # We should revisit this when Istio API exposes a Status that we can rely on.
  # TODO(tcnghia): remove this when https://github.com/istio/istio/issues/882 is fixed.
  echo ">> Patching Istio"
  kubectl patch hpa -n istio-system knative-ingressgateway --patch '{"spec": {"maxReplicas": 1}}'
  # TODO(#2122): Re-enable once we have monitoring e2e.
  # create_monitoring
}

function delete_istio() {
  echo ">> Bringing down Istio"
  kubectl delete --ignore-not-found=true -f ${ISTIO_YAML}
  kubectl delete clusterrolebinding cluster-admin-binding
}

function delete_monitoring() {
  echo ">> Bringing down Monitoring"
  kubectl delete --ignore-not-found=true -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common
}

function delete_everything() {
  # TODO(#2122): Re-enable once we have monitoring e2e.
  # delete_monitoring
  echo ">> Bringing down Serving"
  ko delete --ignore-not-found=true -f config/
  kubectl delete --ignore-not-found=true -f third_party/config/build/release.yaml
  delete_istio
}

function wait_until_cluster_up() {
  wait_until_pods_running knative-serving || fail_test "Knative Serving is not up"
  wait_until_pods_running istio-system || fail_test "Istio system is not up"
  wait_until_service_has_external_ip istio-system knative-ingressgateway || fail_test "Ingress has no external IP"
}
