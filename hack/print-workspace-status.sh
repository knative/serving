#!/bin/bash
# Copyright 2018 Google LLC
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

: ${DOCKER_REPO_OVERRIDE:?"DOCKER_REPO_OVERRIDE is empty or unset.  Please fix and run 'bazel clean' (see README.md)."}
: ${K8S_CLUSTER_OVERRIDE:?"K8S_CLUSTER_OVERRIDE is empty or unset.  Please fix and run 'bazel clean' (see README.md)."}
: ${K8S_USER_OVERRIDE:?"K8S_USER_OVERRIDE is empty or unset.  Please fix and run 'bazel clean' (see REAMDE.md)."}

cat <<EOF
STABLE_K8S_USER ${K8S_USER_OVERRIDE:-}
STABLE_DOCKER_REPO ${DOCKER_REPO_OVERRIDE:-}
STABLE_K8S_CLUSTER ${K8S_CLUSTER_OVERRIDE:-}
EOF
