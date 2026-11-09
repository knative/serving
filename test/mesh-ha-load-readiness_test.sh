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
source "$(dirname "$0")/mesh-ha-load-experiment.sh"
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

# The hashed Deployment name is taken from the failed experiment. The stable
# service label is preserved even when the Deployment name loses its prefix.
cat > "${workdir}/ready.json" <<'EOF'
{"items":[{"metadata":{"name":"autoscale-sustaining0e816942cbb0d0b421178086ef78afb0-deployment","labels":{"serving.knative.dev/service":"autoscale-sustaining-aggregation-linear-tykanjjv"}},"status":{"readyReplicas":11}}]}
EOF
mesh_load_is_ready "${workdir}/ready.json"

jq '.items[0].status.readyReplicas = 6' "${workdir}/ready.json" > "${workdir}/not-ready.json"
if mesh_load_is_ready "${workdir}/not-ready.json"; then
  echo "Incorrectly accepted an under-scaled workload" >&2
  exit 1
fi

jq '.items[0].metadata.deletionTimestamp = "2026-11-08T02:32:29Z"' "${workdir}/ready.json" > "${workdir}/deleting.json"
if mesh_load_is_ready "${workdir}/deleting.json"; then
  echo "Incorrectly accepted a deleting Deployment" >&2
  exit 1
fi

jq 'del(.items[0].metadata.labels)' "${workdir}/ready.json" > "${workdir}/unrelated.json"
if mesh_load_is_ready "${workdir}/unrelated.json"; then
  echo "Incorrectly accepted an unrelated Deployment" >&2
  exit 1
fi

jq --slurp '{items: [.[0].items[0], .[1].items[0]]}' \
  "${workdir}/deleting.json" "${workdir}/not-ready.json" > "${workdir}/mixed.json"
if mesh_load_is_ready "${workdir}/mixed.json"; then
  echo "Incorrectly accepted old/deleting readiness during a new revision startup" >&2
  exit 1
fi

echo "PASS: workload readiness uses stable labels and ignores deleting Deployments"
