#!/usr/bin/env bash

# Copyright 2020 The Knative Authors
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

set -o errexit
set -o nounset
set -o pipefail

if [ -z "${GOPATH:-}" ]; then
  export GOPATH=$(go env GOPATH)
fi

readonly REPO_ROOT=$(dirname $0)/..

source ${REPO_ROOT}/vendor/knative.dev/test-infra/scripts/library.sh

go install ${REPO_ROOT}/cmd/deploy-gen


# Share the common sections of the yaml for serving components, which may
# sit on the dataplane and be considered critical to application function.
readonly COMMON_NON_CRITICAL=$(cat <<EOF
namespace: knative-serving
metricsDomain: knative.dev/serving
safeToEvict: true
EOF
)
readonly COMMON_CRITICAL=$(cat <<EOF
namespace: knative-serving
metricsDomain: knative.dev/serving
safeToEvict: false
EOF
)

deploy-gen > "${REPO_ROOT}/config/core/deployments/controller.yaml" <<EOF
component: controller
importPath: knative.dev/serving/cmd/controller
resources:
  requests:
    cpu: 100m
    memory: 100Mi
  limits:
    cpu: 1000m
    memory: 1000Mi
$COMMON_NON_CRITICAL
EOF

deploy-gen > "${REPO_ROOT}/config/core/deployments/webhook.yaml" <<EOF
component: webhook
importPath: knative.dev/serving/cmd/webhook
resources:
  requests:
    cpu: 20m
    memory: 20Mi
  limits:
    cpu: 200m
    memory: 200Mi
extraPorts:
- name: https-webhook
  port: 443
  targetPort: 8443
$COMMON_CRITICAL
EOF

deploy-gen > "${REPO_ROOT}/config/core/deployments/autoscaler.yaml" <<EOF
component: autoscaler
importPath: knative.dev/serving/cmd/autoscaler
resources:
  requests:
    cpu: 30m
    memory: 40Mi
  limits:
    cpu: 300m
    memory: 400Mi
extraPorts:
- name: http-websocket
  port: 8080
  targetPort: 8080
- name: https
  port: 443
  # This must match ./cmd/autoscaler/main.go
  # It is the port on which we serve custom metrics.
  targetPort: 8443
probePort: 8080
$COMMON_CRITICAL
EOF

deploy-gen > "${REPO_ROOT}/config/core/deployments/activator.yaml" <<EOF
component: activator
importPath: knative.dev/serving/cmd/activator
# The numbers are based on performance test results from
# https://github.com/knative/serving/issues/1625#issuecomment-511930023
resources:
  requests:
    cpu: 300m
    memory: 60Mi
  limits:
    cpu: 1000m
    memory: 600Mi
# The activator is a dataplane component, so we want to give it quite some to exit,
# so that active requests can be safely drained.  The value here should match the
# maximum value allowed for request timeouts, or updates to the activator can result
# in interrupted requests.
terminationGracePeriodSeconds: 300
extraPorts:
- name: http
  port: 80
  targetPort: 8012
- name: http2
  port: 81
  targetPort: 8013
probePort: 8012
$COMMON_CRITICAL
EOF

deploy-gen > "${REPO_ROOT}/config/istio-ingress/controller.yaml" <<EOF
component: networking-istio
importPath: knative.dev/serving/cmd/networking/istio
extraLabels:
  networking.knative.dev/ingress-provider: istio
resources:
  requests:
    cpu: 100m
    memory: 100Mi
  limits:
    cpu: 1000m
    memory: 1000Mi
$COMMON_NON_CRITICAL
EOF

deploy-gen > "${REPO_ROOT}/config/hpa-autoscaling/controller.yaml" <<EOF
component: autoscaler-hpa
importPath: knative.dev/serving/cmd/autoscaler-hpa
extraLabels:
  autoscaling.knative.dev/autoscaler-provider: hpa
resources:
  requests:
    cpu: 30m
    memory: 40Mi
  limits:
    cpu: 300m
    memory: 400Mi
$COMMON_NON_CRITICAL
EOF

deploy-gen > "${REPO_ROOT}/config/namespace-wildcards/controller.yaml" <<EOF
component: networking-ns-cert
importPath: knative.dev/serving/cmd/networking/nscert
extraLabels:
  networking.knative.dev/wildcard-certificate-provider: nscert
resources:
  requests:
    cpu: 100m
    memory: 100Mi
  limits:
    cpu: 1000m
    memory: 1000Mi
$COMMON_NON_CRITICAL
EOF

deploy-gen > "${REPO_ROOT}/config/cert-manager/controller.yaml" <<EOF
component: networking-certmanager
importPath: knative.dev/serving/cmd/networking/certmanager
extraLabels:
  networking.knative.dev/certificate-provider: cert-manager
resources:
  requests:
    cpu: 100m
    memory: 100Mi
  limits:
    cpu: 1000m
    memory: 1000Mi
$COMMON_NON_CRITICAL
EOF
