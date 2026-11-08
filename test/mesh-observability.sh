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

# Temporary, observation-only hooks for the mesh CI investigation. They run
# before installation and finish before kubetest2 deletes/releases the cluster.
function start_test_observability() {
  if [[ "${MESH_OBSERVABILITY:-}" != true ]] || (( ${MESH:-0} == 0 )); then
    return 0
  fi

  MESH_OBSERVABILITY_DIR="${ARTIFACTS}/mesh-observability-$(basename "${E2E_SCRIPT}" .sh)"
  export MESH_OBSERVABILITY_DIR
  mkdir -p "${MESH_OBSERVABILITY_DIR}"

  # Reuse the existing unfiltered, all-namespace Stern logger, but start it
  # before knative_setup so installation and initial sidecar logs are retained.
  start_logging || echo "Unable to start early mesh log collection" >&2

  python3 "${REPO_ROOT_DIR}/test/mesh-observability.py" observe \
    --output "${MESH_OBSERVABILITY_DIR}" --namespace "${SYSTEM_NAMESPACE}" \
    --project "${E2E_PROJECT_ID}" --provider "${CLOUD_PROVIDER}" \
    --interval "${MESH_OBSERVABILITY_INTERVAL:-30}" \
    > "${MESH_OBSERVABILITY_DIR}/observer.log" 2>&1 &
  MESH_OBSERVABILITY_PID=$!
  export MESH_OBSERVABILITY_PID
  # add_trap prepends this hook ahead of the logger/resource teardown hooks.
  add_trap stop_test_observability EXIT
  echo "Mesh observability started: ${MESH_OBSERVABILITY_DIR}"
}

function stop_test_observability() {
  [[ -n "${MESH_OBSERVABILITY_DIR:-}" ]] || return 0
  touch "${MESH_OBSERVABILITY_DIR}/stop"
  for ((i=0; i<30; i++)); do
    [[ -f "${MESH_OBSERVABILITY_DIR}/stopped" ]] && break
    kill -0 "${MESH_OBSERVABILITY_PID}" 2>/dev/null || break
    sleep 1
  done
  if kill -0 "${MESH_OBSERVABILITY_PID}" 2>/dev/null; then
    kill "${MESH_OBSERVABILITY_PID}" 2>/dev/null || true
  fi

  # Export while this job still owns the Boskos project. This is bounded and
  # best-effort: export failures must not change the original test result.
  timeout 200s python3 "${REPO_ROOT_DIR}/test/mesh-observability.py" export \
    --output "${MESH_OBSERVABILITY_DIR}" --namespace "${SYSTEM_NAMESPACE}" \
    --project "${E2E_PROJECT_ID}" --provider "${CLOUD_PROVIDER}" \
    > "${MESH_OBSERVABILITY_DIR}/export.log" 2>&1 \
    || echo "GKE telemetry export incomplete; see ${MESH_OBSERVABILITY_DIR}/export.log" >&2
  echo "Mesh observability saved: ${MESH_OBSERVABILITY_DIR}; check run.json and export-status.json"
}
