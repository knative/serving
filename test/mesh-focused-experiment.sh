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

# Focused fresh-cluster follow-ups, not repetitions of the full original suite.
function run_mesh_focused_experiment() {
  local pattern count mode="${MESH_HA_LOAD_MODE}" result=0
  case "${mode}" in
    owner-handoff) pattern='^TestMeshOwnerHandoffUnderLoad$'; count=1 ;;
    workload-san) pattern='^TestWorkloadHA$/^activator-not-in-path$'; count=3 ;;
    *) echo "Unknown focused mesh mode: ${mode}" >&2; return 2 ;;
  esac
  local dir="${ARTIFACTS}/mesh-focused"
  mkdir -p "${dir}"
  printf '{"mode":"%s","pattern":"%s","count":%d,"pod_references":true,"fresh_cluster":true,"full_suite":false}\n' \
    "${mode}" "${pattern}" "${count}" > "${dir}/experiment.json"
  date -u +%Y-%m-%dT%H:%M:%SZ > "${dir}/started-at.txt"
  go_test_e2e -timeout=30m -failfast -parallel=1 -count="${count}" -run="${pattern}" ./test/ha \
    "${E2E_TEST_FLAGS[@]}" -replicas="${REPLICAS:-3}" -buckets="${BUCKETS:-10}" -spoofinterval=10ms \
    > "${dir}/tests.log" 2>&1 || result=$?
  date -u +%Y-%m-%dT%H:%M:%SZ > "${dir}/finished-at.txt"
  printf '{"mode":"%s","result":%d}\n' "${mode}" "${result}" > "${dir}/result.json"
  tail -n 25 "${dir}/tests.log"
  return "${result}"
}
