#!/usr/bin/env bash

# Copyright 2021 The Knative Authors
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

source $(dirname $0)/../e2e-common.sh

readonly GOOGLE_APPLICATION_CREDENTIALS="/etc/test-account/service-account.json"
readonly GCS_BUCKET="knative-performance"

# Skip installing istio as an add-on.
# Temporarily increasing the cluster size for serving tests to rule out
# resource/eviction as causes of flakiness.
# Pin to 1.20 since scale test is super flakey on 1.21
initialize --skip-istio-addon --min-nodes=4 --max-nodes=4 --enable-ha --cluster-version=1.20 "$@"

header "Updating cluster"

# Update the activator hpa minReplicas to 10
kubectl patch hpa -n "${SYSTEM_NAMESPACE}" activator --patch '{"spec": {"minReplicas": 10}}'

# Update the scale-to-zero grace period to 10s
kubectl patch configmap/config-autoscaler -n "${SYSTEM_NAMESPACE}" \
    --type merge \
    -p '{"data":{"scale-to-zero-grace-period":"10s"}}'

# Ensure gradual rollout is enabled.
kubectl patch configmap/config-network -n "${SYSTEM_NAMESPACE}"\
    --type merge \
    -p '{"data":{"rolloutDuration":"240"}}'

header "Install Kperf"

origdir="$( pwd -P )"
tempdir="$( mktemp -d )"
echo
echo "Temporary files produced are stored at: ${tempdir}"
echo
cd "${tempdir}"
git clone https://github.com/knative-sandbox/kperf.git
cd kperf
./hack/build.sh
PATH="${tempdir}/kperf:${PATH}"
export PATH
cd "${origdir}"

header "Running tests"

TIMEOUT=30m

mkdir -p "${ARTIFACTS}/kperf"

header "Running performance tests"
go_test_e2e -tags=performance -timeout=${TIMEOUT} \
  ./test/performance \
  --services-count=30 \
  --service-timeout=60 \
  --service-average=60 \
  --service-account=${GOOGLE_APPLICATION_CREDENTIALS} \
  --bucket=${GCS_BUCKET} \
  --combine-results=false \
  --upload-results=true \
  --kperf-output="${ARTIFACTS}/kperf" || fail_test

# Remove the kail log file if the test flow passes.
# This is for preventing too many large log files to be uploaded to GCS in CI.
rm "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt"
success
