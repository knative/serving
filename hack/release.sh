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
: ${SERVING_RELEASE_GCS:="knative-releases/serving"}
: ${SERVING_RELEASE_GCR:="gcr.io/knative-releases"}
readonly SERVING_RELEASE_GCS
readonly SERVING_RELEASE_GCR

# istio.yaml file to upload
# We publish our own istio.yaml, so users don't need to use helm"
readonly ISTIO_YAML=./third_party/istio-1.0-prerelease/istio.yaml
# Elasticsearch and Kibana yaml file.
readonly OBSERVABILITY_ELK_YAML=observability_elk.yaml
# Prometheus and Grafana yaml file.
readonly OBSERVABILITY_PROMETHEUS_YAML=observability_prom.yaml
# Prometheus and Grafana yaml file.
readonly OBSERVABILITY_ZIPKIN_YAML=observability_zipkin.yaml
# Release with Build + Serving
readonly OUTPUT_YAML=release.yaml

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

echo "Building Knative Serving"
echo "---" >> ${OUTPUT_YAML}
ko resolve ${KO_FLAGS} -f config/ >> ${OUTPUT_YAML}

echo "Building Observability - Elasticsearch & Kibana"
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-namespace.yaml \
    -f third_party/config/monitoring/logging/elasticsearch \
    -f config/monitoring/logging/elasticsearch > ${OBSERVABILITY_ELK_YAML}

echo "Building Observability - Prometheus & Grafana"
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-namespace.yaml \
    -f third_party/config/monitoring/metrics/prometheus \
    -f config/monitoring/metrics/prometheus > ${OBSERVABILITY_PROMETHEUS_YAML}

echo "Building Observability - Zipkin"
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-namespace.yaml \
    -f config/monitoring/tracing/zipkin > ${OBSERVABILITY_ZIPKIN_YAML}

tag_images_in_yaml ${OUTPUT_YAML} ${SERVING_RELEASE_GCR} ${TAG}

echo "New release built successfully"

if (( ! PUBLISH_RELEASE )); then
 exit 0
fi

# Publish the release

for yaml in ${OUTPUT_YAML} ${OBSERVABILITY_ELK_YAML} ${OBSERVABILITY_PROMETHEUS_YAML} ${OBSERVABILITY_ZIPKIN_YAML} ${ISTIO_YAML}; do
  echo "Publishing ${yaml}"
  publish_yaml ${yaml} ${SERVING_RELEASE_GCS} ${TAG}
done

echo "New release published successfully"
