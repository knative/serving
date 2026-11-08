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

set -euo pipefail
source "$(dirname "$0")/mesh-focused-experiment.sh"
ARTIFACTS="$(mktemp -d)"
trap 'rm -rf "${ARTIFACTS}"' EXIT
E2E_TEST_FLAGS=(-ingress-class=istio.ingress.networking.knative.dev)
FAKE_RESULT=0
function go_test_e2e() {
  printf '%s\n' "$@" > "${ARTIFACTS}/args.txt"
  return "${FAKE_RESULT}"
}
MESH_HA_LOAD_MODE=owner-handoff
run_mesh_focused_experiment
grep -Fx -- '-run=^TestMeshOwnerHandoffUnderLoad$' "${ARTIFACTS}/args.txt"
grep -Fx -- '-count=1' "${ARTIFACTS}/args.txt"
MESH_HA_LOAD_MODE=workload-san
run_mesh_focused_experiment
grep -Fx -- '-run=^TestWorkloadHA$/^activator-not-in-path$' "${ARTIFACTS}/args.txt"
grep -Fx -- '-count=3' "${ARTIFACTS}/args.txt"
grep -Fx -- '-timeout=30m' "${ARTIFACTS}/args.txt"
jq -e '.full_suite == false and .pod_references == true and .count == 3' "${ARTIFACTS}/mesh-focused/experiment.json"
FAKE_RESULT=1
if run_mesh_focused_experiment; then echo 'Failed tests incorrectly passed' >&2; exit 1; fi
jq -e '.result == 1' "${ARTIFACTS}/mesh-focused/result.json"
echo 'PASS: focused selectors, counts, unchanged timeout and failure propagation'
