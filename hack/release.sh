#!/bin/bash

# Copyright 2018 Google, Inc. All rights reserved.
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
set -o pipefail

source "$(dirname $(readlink -f ${BASH_SOURCE}))/../test/library.sh"

# Set default GCS/GCR
: ${SERVING_RELEASE_GCS:="knative-releases"}
: ${SERVING_RELEASE_GCR:="gcr.io/knative-releases"}
readonly SERVING_RELEASE_GCS
readonly SERVING_RELEASE_GCR

# Istio.yaml file to upload
readonly ISTIO_VERSION=0.8.0
readonly ISTIO_DIR=./third_party/istio-${ISTIO_VERSION}/

# Local generated yaml file.
readonly OUTPUT_YAML=release.yaml

function cleanup() {
  restore_override_vars
}

function banner() {
  echo "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"
  echo "@@@@ $1 @@@@"
  echo "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"
}

# Tag Knative Serving images in the yaml file with a tag.
# Parameters: $1 - yaml file to parse for images.
#             $2 - tag to apply.
function tag_knative_images() {
  [[ -z $2 ]] && return 0
  echo "Tagging images with $2"
  for image in $(grep -o "${DOCKER_REPO_OVERRIDE}/[a-z\./-]\+@sha256:[0-9a-f]\+" $1); do
    gcloud -q container images add-tag ${image} ${image%%@*}:$2
  done
}

cd ${SERVING_ROOT_DIR}
trap cleanup EXIT

if [[ "$1" != "--skip-tests" ]]; then
  banner "RUNNING RELEASE VALIDATION TESTS"
  # Run tests.
  ./test/presubmit-tests.sh
fi

install_ko

banner "    BUILDING THE RELEASE   "

# Set the repository
export KO_DOCKER_REPO=${SERVING_RELEASE_GCR}
# Build should not try to deploy anything, use a bogus value for cluster.
export K8S_CLUSTER_OVERRIDE=CLUSTER_NOT_SET
export K8S_USER_OVERRIDE=USER_NOT_SET
export DOCKER_REPO_OVERRIDE=DOCKER_NOT_SET

# If this is a prow job,
TAG=""
if (( IS_PROW )); then
  # Authenticate against GCR.
  gcr_auth
  commit=$(git describe --tags --always --dirty)
  # Like kubernetes, image tag is vYYYYMMDD-commit
  TAG="v$(date +%Y%m%d)-${commit}"
fi
readonly TAG

echo "- Destination GCR: ${SERVING_RELEASE_GCR}"
echo "- Destination GCS: ${SERVING_RELEASE_GCS}"

echo "Copying Build release"
cp ${SERVING_ROOT_DIR}/third_party/config/build/release.yaml ${OUTPUT_YAML}
echo "---" >> ${OUTPUT_YAML}

echo "Building Knative Serving"
ko resolve -f config/ >> ${OUTPUT_YAML}
echo "---" >> ${OUTPUT_YAML}

echo "Building Monitoring & Logging"
# Use ko to concatenate them all together.
ko resolve -R -f config/monitoring/100-common \
    -f config/monitoring/150-prod \
    -f third_party/config/monitoring \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml >> ${OUTPUT_YAML}

tag_knative_images ${OUTPUT_YAML} ${TAG}

echo "Publishing release.yaml"
gsutil cp ${OUTPUT_YAML} gs://${SERVING_RELEASE_GCS}/latest/release.yaml
if [[ -n ${TAG} ]]; then
  gsutil cp ${OUTPUT_YAML} gs://${SERVING_RELEASE_GCS}/previous/${TAG}/
fi

echo "Publishing our istio.yaml, so users don't need to use helm."
gsutil cp ${ISTIO_DIR}/istio.yaml gs://${SERVING_RELEASE_GCS}/latest/istio.yaml

echo "New release published successfully"

# TODO(mattmoor): Create other aliases?
