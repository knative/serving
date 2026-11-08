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

# Serving can hash/truncate Deployment names. Match the service label instead,
# and do not use readiness from a Deployment already being torn down.
function mesh_load_is_ready() {
  jq -e '.items | any(
    (.metadata.deletionTimestamp == null) and
    ((.metadata.labels["serving.knative.dev/service"] // "") | startswith("autoscale-sustaining-")) and
    ((.status.readyReplicas // 0) >= 7)
  )' "$1" > /dev/null
}

# Focused experiment, not the original periodic workload. Only the sustained
# autoscaling test runs beside HA; no global feature-toggle suites run here.
function run_mesh_ha_load_experiment() {
  local mode="${MESH_HA_LOAD_MODE}"
  case "${mode}" in
    overlap|load-only|ha-only) ;;
    *) echo "Unknown mesh HA/load mode: ${mode}" >&2; return 2 ;;
  esac
  local load_pid="" load_result=0 ha_result=0 ready=0 ha_started=0
  local dir="${ARTIFACTS}/mesh-ha-load"
  mkdir -p "${dir}"
  printf '{"mode":"%s","load_test":"TestAutoscaleSustaining","load_count":2,"ha_test":"TestActivatorHAGraceful","fresh_cluster":true}\n' \
    "${mode}" > "${dir}/experiment.json"
  echo "Mesh experiment: ${mode}. Full-suite baseline was c30f08250."

  if [[ "${mode}" != ha-only ]]; then
    # Two repetitions cover a full graceful-HA run, including its timeout path.
    # Keep the original package timeout and within-package parallelism.
    go_test_e2e -timeout=50m "${GO_TEST_FLAGS[@]}" -count=2 \
      -run='^TestAutoscaleSustaining$' ./test/e2e "${E2E_TEST_FLAGS[@]}" \
      > "${dir}/load.log" 2>&1 &
    load_pid=$!
    echo "Autoscaling load started; output is ${dir}/load.log"
  fi

  if [[ "${mode}" == overlap ]]; then
    # Initial scale is one. Seven ready replicas proves the sustained load has
    # actually driven scale-up, instead of merely launching a test process.
    local deadline=$((SECONDS + 180))
    while (( SECONDS < deadline )); do
      if ! kill -0 "${load_pid}" 2>/dev/null || grep -q '^FAIL .*TestAutoscaleSustaining' "${dir}/load.log"; then
        break
      fi
      if timeout 10s kubectl --request-timeout=5s get deployments -n "${TEST_NAMESPACE:-serving-tests}" -o json \
        > "${dir}/load-deployments.json" 2> "${dir}/load-deployments.stderr"; then
        if mesh_load_is_ready "${dir}/load-deployments.json"; then
          ready=1
          date -u +%Y-%m-%dT%H:%M:%SZ > "${dir}/load-ready-at.txt"
          echo "Sustained load has at least seven ready replicas; starting graceful HA"
          break
        fi
      fi
      sleep 5
    done
    if (( ! ready )); then
      echo "Load did not reach the prerequisite scale; HA was not started" >&2
      ha_result=1
    fi
  fi

  if [[ "${mode}" == ha-only ]] || { [[ "${mode}" == overlap ]] && (( ready )); }; then
    ha_started=1
    date -u +%Y-%m-%dT%H:%M:%SZ > "${dir}/ha-started-at.txt"
    if go_test_e2e -timeout=30m -failfast -parallel=1 -run='^TestActivatorHAGraceful$' ./test/ha \
      "${E2E_TEST_FLAGS[@]}" -replicas="${REPLICAS:-3}" -buckets="${BUCKETS:-10}" -spoofinterval=10ms; then
      ha_result=0
    else
      ha_result=$?
    fi
    date -u +%Y-%m-%dT%H:%M:%SZ > "${dir}/ha-finished-at.txt"
  fi

  if [[ -n "${load_pid}" ]]; then
    # Do not kill the load test to manufacture a failure or leak resources.
    # Both suites must finish cleanly for the experiment to pass.
    if wait "${load_pid}"; then
      load_result=0
    else
      load_result=$?
    fi
    tail -n 15 "${dir}/load.log"
  fi
  printf '{"mode":"%s","load_result":%d,"ha_result":%d,"load_ready":%d,"ha_started":%d}\n' \
    "${mode}" "${load_result}" "${ha_result}" "${ready}" "${ha_started}" > "${dir}/result.json"
  echo "Mesh experiment result: load=${load_result}, HA=${ha_result}"
  (( load_result == 0 && ha_result == 0 ))
}
