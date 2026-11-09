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

# Opt-in only. Infra enables this on istio-latest-mesh_serving_main_periodic.
function start_mesh_diagnostics() {
  [[ "${MESH_DIAGNOSTICS:-false}" == true ]] && (( ${MESH:-0} )) || return 0
  local output="${ARTIFACTS}/mesh-diagnostics-$(basename "${E2E_SCRIPT}" .sh)"
  local binary="${TMP_DIR}/mesh-diagnostics"
  mkdir -p "${output}"
  # Match the existing e2e logger's pinned Stern version.
  if ! command -v stern >/dev/null; then
    go install github.com/stern/stern@v1.33.0 || return 0
  fi
  go build -o "${binary}" "${REPO_ROOT_DIR}/test/meshdiagnostics" || return 0
  "${binary}" --output="${output}" --namespace="${SYSTEM_NAMESPACE}" \
    --test-namespace="${TEST_NAMESPACE:-serving-tests}" --script="${E2E_SCRIPT}" \
    > "${output}/observer.log" 2>&1 &
  MESH_DIAGNOSTICS_PID=$!
  # If startup fails, test_setup falls back to its ordinary logger.
  add_trap 'stop_mesh_diagnostics || true' EXIT SIGTERM
}

function stop_mesh_diagnostics() {
  [[ -n "${MESH_DIAGNOSTICS_PID:-}" ]] || return 0
  kill -TERM "${MESH_DIAGNOSTICS_PID}" 2>/dev/null || true
  # Let bounded captures archive their partial results before cluster teardown.
  for ((i=0;i<100;i++)); do
    kill -0 "${MESH_DIAGNOSTICS_PID}" 2>/dev/null || break
    sleep 1
  done
  kill -KILL "${MESH_DIAGNOSTICS_PID}" 2>/dev/null || true
  wait "${MESH_DIAGNOSTICS_PID}" 2>/dev/null || true
  unset MESH_DIAGNOSTICS_PID
}
