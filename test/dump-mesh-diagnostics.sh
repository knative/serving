#!/usr/bin/env bash

# Copyright 2026 The Knative Authors
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

# Collect unfiltered logs and mesh state before subsequent tests restart pods
# or change configuration. Individual failures must not stop collection or
# change the test result. Do not collect Secrets or Envoy secret dumps.
set -u

: "${ARTIFACTS:?ARTIFACTS must be set}"
: "${SYSTEM_NAMESPACE:?SYSTEM_NAMESPACE must be set}"

phase="${1:-failure}"
workdir="$(mktemp -d)" || exit 1
mkdir -p "${ARTIFACTS}"
archive="${ARTIFACTS}/mesh-diagnostics-${phase}.tar.gz"
function finish_capture() {
  # Preserve partial captures on TERM as well as normal completion.
  if tar -czf "${archive}" -C "${workdir}" .; then
    rm -rf "${workdir}"
    echo "Mesh diagnostics written to ${archive}"
  else
    echo "Unable to archive mesh diagnostics from ${workdir}" >&2
  fi
}
trap finish_capture EXIT
trap 'exit 143' TERM
kubectl_cmd=(kubectl --request-timeout=10s)

function capture() {
  local file="$1"
  shift
  timeout 15s "$@" > "${workdir}/${file}" 2> "${workdir}/${file}.stderr" \
    || echo "Mesh diagnostic collection failed: ${file}" >&2
}

date -u > "${workdir}/collected-at.txt"
capture nodes.yaml "${kubectl_cmd[@]}" get nodes -o yaml

for namespace in "${SYSTEM_NAMESPACE}" istio-system "${TEST_NAMESPACE:-serving-tests}"; do
  capture "${namespace}-resources.yaml" "${kubectl_cmd[@]}" get \
    pods,services,endpointslices,leases -n "${namespace}" -o yaml
  capture "${namespace}-events.yaml" "${kubectl_cmd[@]}" get events -n "${namespace}" -o yaml
  capture "${namespace}-pods.json" "${kubectl_cmd[@]}" get pods -n "${namespace}" -o json
  capture "${namespace}-istio.yaml" "${kubectl_cmd[@]}" get \
    virtualservices.networking.istio.io,destinationrules.networking.istio.io,peerauthentications.security.istio.io \
    -n "${namespace}" -o yaml

  while IFS=$'\t' read -r pod app containers restarts; do
    [[ -n "${pod}" ]] || continue
    capture "${namespace}-${pod}.log" "${kubectl_cmd[@]}" logs -n "${namespace}" "${pod}" \
      --all-containers=true --prefix --timestamps --since=1h --tail=20000
    if (( restarts > 0 )); then
      capture "${namespace}-${pod}-previous.log" "${kubectl_cmd[@]}" logs -n "${namespace}" "${pod}" \
        --all-containers=true --prefix --timestamps --previous --tail=2000
    fi

    # Include native sidecars, which are represented as init containers.
    if [[ " ${containers} " == *" istio-proxy "* ]] && \
      [[ "${app}" == autoscaler || "${app}" == activator || "${app}" == istio-ingressgateway ]]; then
      # EDS, CDS and RDS identify stale endpoints, missing tlsMode metadata and
      # incorrect routing. Omitting SDS avoids publishing workload certificates.
      capture "${namespace}-${pod}-envoy-config.json" "${kubectl_cmd[@]}" exec -n "${namespace}" "${pod}" \
        -c istio-proxy -- pilot-agent request GET 'config_dump?include_eds&resource=dynamic_active_clusters'
      capture "${namespace}-${pod}-envoy-endpoints.json" "${kubectl_cmd[@]}" exec -n "${namespace}" "${pod}" \
        -c istio-proxy -- pilot-agent request GET 'config_dump?include_eds&resource=dynamic_endpoint_configs'
      capture "${namespace}-${pod}-envoy-routes.json" "${kubectl_cmd[@]}" exec -n "${namespace}" "${pod}" \
        -c istio-proxy -- pilot-agent request GET 'config_dump?resource=dynamic_route_configs'
      capture "${namespace}-${pod}-envoy-listeners.json" "${kubectl_cmd[@]}" exec -n "${namespace}" "${pod}" \
        -c istio-proxy -- pilot-agent request GET 'config_dump?resource=dynamic_listeners'
      capture "${namespace}-${pod}-envoy-stats.txt" "${kubectl_cmd[@]}" exec -n "${namespace}" "${pod}" \
        -c istio-proxy -- pilot-agent request GET stats
    fi
  done < <(jq -r --arg target "${MESH_DIAGNOSTIC_TARGET_POD:-}" '.items | sort_by(if .metadata.name == $target then 0 elif .metadata.labels.app == "autoscaler" then 1 elif .metadata.labels.app == "activator" then 2 else 3 end) | .[] | [.metadata.name, (.metadata.labels.app // "unknown"), ([.spec.containers[]?.name, .spec.initContainers[]?.name] | join(" ")), ([.status.containerStatuses[]?.restartCount, .status.initContainerStatuses[]?.restartCount] | add // 0)] | @tsv' \
    "${workdir}/${namespace}-pods.json")
done

capture serving-config.yaml "${kubectl_cmd[@]}" get configmaps -n "${SYSTEM_NAMESPACE}" \
  config-autoscaler config-leader-election config-network config-istio config-logging -o yaml
capture istio-config.yaml "${kubectl_cmd[@]}" get configmap istio -n istio-system -o yaml

# finish_capture archives complete or interrupted results on EXIT.
