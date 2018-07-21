#!/bin/bash

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

# Load github.com/knative/test-infra/images/prow-tests/scripts/release.sh
[ -f /workspace/release.sh ] \
  && source /workspace/release.sh \
  || eval "$(docker run --entrypoint sh gcr.io/knative-tests/test-infra/prow-tests -c 'cat release.sh')"
[ -v KNATIVE_TEST_INFRA ] || exit 1

# Set default GCS/GCR
: ${SERVING_RELEASE_GCS:="knative-releases"}
: ${SERVING_RELEASE_GCR:="gcr.io/knative-releases"}
readonly SERVING_RELEASE_GCS
readonly SERVING_RELEASE_GCR

# istio.yaml file to upload
# We publish our own istio.yaml, so users don't need to use helm"
readonly ISTIO_YAML=./third_party/istio-0.8.0/istio.yaml
# Local generated yaml file.
readonly OUTPUT_YAML=release.yaml
# Local generated lite yaml file.
readonly LITE_YAML=release-lite.yaml
# Local generated yaml file without the logging and monitoring components.
readonly NO_MON_YAML=release-no-mon.yaml

# Script entry point

parse_flags $@

set -o errexit
set -o pipefail

run_validation_tests ./test/presubmit-tests.sh

banner "Building the release"

# Set the repository
export KO_DOCKER_REPO=${SERVING_RELEASE_GCR}
# Build should not try to deploy anything, use a bogus value for cluster.
export K8S_CLUSTER_OVERRIDE=CLUSTER_NOT_SET
export K8S_USER_OVERRIDE=USER_NOT_SET
export DOCKER_REPO_OVERRIDE=DOCKER_NOT_SET

if (( PUBLISH_RELEASE )); then
  echo "- Destination GCR: ${SERVING_RELEASE_GCR}"
  echo "- Destination GCS: ${SERVING_RELEASE_GCS}"
fi

# Build the release

echo "Copying Build release"
cp ${REPO_ROOT_DIR}/third_party/config/build/release.yaml ${OUTPUT_YAML}
echo "---" >> ${OUTPUT_YAML}

echo "Building Knative Serving"
ko resolve ${KO_FLAGS} -f config/ >> ${OUTPUT_YAML}
echo "---" >> ${OUTPUT_YAML}

echo "Building Monitoring & Logging"
# Make a copy for the lite version
cp ${OUTPUT_YAML} ${LITE_YAML}
# Make a copy for the no monitoring version
cp ${OUTPUT_YAML} ${NO_MON_YAML}
# Use ko to concatenate them all together.
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-prod \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml >> ${OUTPUT_YAML}
# Use ko to do the same for the lite version.
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-common \
    -f third_party/config/monitoring/common/istio \
    -f third_party/config/monitoring/common/kubernetes/kube-state-metrics \
    -f third_party/config/monitoring/common/prometheus-operator \
    -f config/monitoring/150-elasticsearch-prod/100-scaling-configmap.yaml \
    -f config/monitoring/200-common/100-fluentd.yaml \
    -f config/monitoring/200-common/100-grafana-dash-knative-efficiency.yaml \
    -f config/monitoring/200-common/100-grafana-dash-knative.yaml \
    -f config/monitoring/200-common/100-grafana.yaml \
    -f config/monitoring/200-common/100-istio.yaml \
    -f config/monitoring/200-common/200-prometheus-exporter \
    -f config/monitoring/200-common/300-prometheus \
    -f config/monitoring/200-common/100-istio.yaml >> ${LITE_YAML}

tag_images_in_yaml ${OUTPUT_YAML} ${SERVING_RELEASE_GCR} ${TAG}

echo "New release built successfully"

if (( ! PUBLISH_RELEASE )); then
 exit 0
fi

# Publish the release

for yaml in ${OUTPUT_YAML} ${LITE_YAML} ${NO_MON_YAML} ${ISTIO_YAML}; do
  echo "Publishing ${yaml}"
  publish_yaml ${yaml} ${SERVING_RELEASE_GCS} ${TAG}
done

echo "New release published successfully"
