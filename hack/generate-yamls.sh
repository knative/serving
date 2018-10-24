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

REPO_ROOT_DIR=$1
GENERATED_YAML_LIST=$2

# istio.yaml file to upload
# We publish our own istio.yaml, so users don't need to use helm
readonly ISTIO_CRD_YAML=./third_party/istio-1.0.2/istio-crds.yaml
readonly ISTIO_YAML=./third_party/istio-1.0.2/istio.yaml
readonly ISTIO_LEAN_YAML=./third_party/istio-1.0.2/istio-lean.yaml

readonly BUILD_YAML=build.yaml
readonly SERVING_YAML=serving.yaml
readonly MONITORING_YAML=monitoring.yaml
readonly MONITORING_METRIC_PROMETHEUS_YAML=monitoring-metrics-prometheus.yaml
readonly MONITORING_TRACE_ZIPKIN_YAML=monitoring-tracing-zipkin.yaml
readonly MONITORING_TRACE_ZIPKIN_IN_MEM_YAML=monitoring-tracing-zipkin-in-mem.yaml
readonly MONITORING_LOG_ELASTICSEARCH_YAML=monitoring-logs-elasticsearch.yaml

# Build the release

echo "Copying Build release"
cp "${REPO_ROOT_DIR}/third_party/config/build/release.yaml" "${BUILD_YAML}"

echo "Building Knative Serving"
ko resolve ${KO_FLAGS} -f config/ > "${SERVING_YAML}"

echo "Building Monitoring & Logging"
# Use ko to concatenate them all together.
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-namespace.yaml \
    -f third_party/config/monitoring/logging/elasticsearch \
    -f config/monitoring/logging/elasticsearch \
    -f third_party/config/monitoring/metrics/prometheus \
    -f config/monitoring/metrics/prometheus \
    -f config/monitoring/tracing/zipkin > "${MONITORING_YAML}"

# Metrics via Prometheus & Grafana
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-namespace.yaml \
    -f third_party/config/monitoring/metrics/prometheus \
    -f config/monitoring/metrics/prometheus > "${MONITORING_METRIC_PROMETHEUS_YAML}"

# Logs via ElasticSearch, Fluentd & Kibana
ko resolve ${KO_FLAGS} -R -f config/monitoring/100-namespace.yaml \
    -f third_party/config/monitoring/logging/elasticsearch \
    -f config/monitoring/logging/elasticsearch > "${MONITORING_LOG_ELASTICSEARCH_YAML}"

# Traces via Zipkin when ElasticSearch is installed
ko resolve ${KO_FLAGS} -R -f config/monitoring/tracing/zipkin > "${MONITORING_TRACE_ZIPKIN_YAML}"

# Traces via Zipkin in Memory when ElasticSearch is not installed
ko resolve ${KO_FLAGS} -R -f config/monitoring/tracing/zipkin-in-mem >> "${MONITORING_TRACE_ZIPKIN_IN_MEM_YAML}"

echo "Building Release Bundles."

# These are the "bundled" yaml files that we publish.
# Local generated yaml file.
readonly RELEASE_YAML=release.yaml
# Local generated lite yaml file.
readonly LITE_YAML=release-lite.yaml
# Local generated yaml file without the logging and monitoring components.
readonly NO_MON_YAML=release-no-mon.yaml

# NO_MON is just build and serving
cp "${BUILD_YAML}" "${NO_MON_YAML}"
echo "---" >> "${NO_MON_YAML}"
cat "${SERVING_YAML}" >> "${NO_MON_YAML}"
echo "---" >> "${NO_MON_YAML}"

# LITE is NO_MON plus "lean" monitoring
cp "${NO_MON_YAML}" "${LITE_YAML}"
echo "---" >> "${LITE_YAML}"
cat "${MONITORING_METRIC_PROMETHEUS_YAML}" >> "${LITE_YAML}"
echo "---" >> "${LITE_YAML}"

# RELEASE is NO_MON plus full monitoring
cp "${NO_MON_YAML}" "${RELEASE_YAML}"
echo "---" >> "${RELEASE_YAML}"
cat "${MONITORING_YAML}" >> "${RELEASE_YAML}"
echo "---" >> "${RELEASE_YAML}"

readonly YAMLS_TO_PUBLISH="${RELEASE_YAML} ${LITE_YAML} ${NO_MON_YAML} ${SERVING_YAML} ${BUILD_YAML} ${MONITORING_YAML} ${MONITORING_METRIC_PROMETHEUS_YAML} ${MONITORING_LOG_ELASTICSEARCH_YAML} ${MONITORING_TRACE_ZIPKIN_YAML} ${MONITORING_TRACE_ZIPKIN_IN_MEM_YAML} ${ISTIO_CRD_YAML} ${ISTIO_YAML} ${ISTIO_LEAN_YAML}"

echo $YAMLS_TO_PUBLISH | sed "s/ /\n/g" > $GENERATED_YAML_LIST
