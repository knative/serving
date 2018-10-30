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

# Set default GCS/GCR
: ${SERVING_RELEASE_GCS:="knative-releases/serving"}
: ${SERVING_RELEASE_GCR:="gcr.io/knative-releases"}
readonly SERVING_RELEASE_GCS
readonly SERVING_RELEASE_GCR

# Script entry point

initialize $@

set -o errexit
set -o pipefail

run_validation_tests ./test/presubmit-tests.sh

banner "Building the release"

# Set the repository
export KO_DOCKER_REPO="${SERVING_RELEASE_GCR}"
# Build should not try to deploy anything, use a bogus value for cluster.
export K8S_CLUSTER_OVERRIDE=CLUSTER_NOT_SET
export K8S_USER_OVERRIDE=USER_NOT_SET
export DOCKER_REPO_OVERRIDE=DOCKER_NOT_SET

if (( PUBLISH_RELEASE )); then
  echo "- Destination GCR: ${SERVING_RELEASE_GCR}"
  echo "- Destination GCS: ${SERVING_RELEASE_GCS}"
fi

# Build the release
#
# Run this generate-yamls.sh script, which should be versioned with the
# branch since the detail of building may change over time.
YAML_LIST="generated-yamls.txt"

$(dirname $0)/generate-yamls.sh "${REPO_ROOT_DIR}" "${YAML_LIST}" || abort "Cannot build the release."
YAMLS_TO_PUBLISH=$(cat "${YAML_LIST}")
RELEASE_YAML=$(head -n1 "${YAML_LIST}")

echo "Tagging referenced images with ${TAG}."

tag_images_in_yaml "${RELEASE_YAML}" "${SERVING_RELEASE_GCR}" "${TAG}"

echo "New release built successfully"

if (( ! PUBLISH_RELEASE )); then
 exit 0
fi

# Publish the release
for yaml in ${YAMLS_TO_PUBLISH}; do
  echo "Publishing ${yaml}"
  publish_yaml "${yaml}" "${SERVING_RELEASE_GCS}" "${TAG}"
done

branch_release "Knative Serving" ${YAMLS_TO_PUBLISH}

echo "New release published successfully"
