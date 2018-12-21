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

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/release.sh

# Script entry point

initialize $@

set -o errexit
set -o pipefail

run_validation_tests ./test/presubmit-tests.sh

# Build the release

banner "Building the release"

# Build should not try to deploy anything, use a bogus value for cluster.
export K8S_CLUSTER_OVERRIDE=CLUSTER_NOT_SET
export K8S_USER_OVERRIDE=USER_NOT_SET
export DOCKER_REPO_OVERRIDE=DOCKER_NOT_SET

# Run `generate-yamls.sh`, which should be versioned with the
# branch since the detail of building may change over time.
readonly YAML_LIST="$(mktemp)"
$(dirname $0)/generate-yamls.sh "${REPO_ROOT_DIR}" "${YAML_LIST}"
readonly YAMLS_TO_PUBLISH=$(cat "${YAML_LIST}" | tr '\n' ' ')
readonly RELEASE_YAML="$(head -n1 ${YAML_LIST})"

tag_images_in_yaml "${RELEASE_YAML}"

echo "New release built successfully"

if (( ! PUBLISH_RELEASE )); then
  # Copy the generated YAML files to the repo root dir.
  cp ${YAMLS_TO_PUBLISH} ${REPO_ROOT_DIR}
  exit 0
fi

# Publish the release
# We publish our own istio.yaml, so users don't need to use helm
for yaml in ${YAMLS_TO_PUBLISH}; do
  publish_yaml "${yaml}"
done

branch_release "Knative Serving" "${YAMLS_TO_PUBLISH}"

echo "New release published successfully"
