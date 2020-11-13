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

# This script builds all the YAMLs that Knative serving publishes. It may be
# varied between different branches, of what it does, but the following usage
# must be observed:
#
# generate-yamls.sh  <repo-root-dir> <generated-yaml-list>
#     repo-root-dir         the root directory of the repository.
#     generated-yaml-list   an output file that will contain the list of all
#                           YAML files. The first file listed must be our
#                           manifest that contains all images to be tagged.

# Different versions of our scripts should be able to call this script with
# such assumption so that the test/publishing/tagging steps can evolve
# differently than how the YAMLs are built.

# The following environment variables affect the behavior of this script:
# * `$KO_FLAGS` Any extra flags that will be passed to ko.
# * `$YAML_OUTPUT_DIR` Where to put the generated YAML files, otherwise a
#   random temporary directory will be created. **All existing YAML files in
#   this directory will be deleted.**
# * `$KO_DOCKER_REPO` If not set, use ko.local as the registry.

set -o errexit
set -o pipefail

readonly YAML_REPO_ROOT=${1:?"First argument must be the repo root dir"}
readonly YAML_LIST_FILE=${2:?"Second argument must be the output file"}

# Set output directory
if [[ -z "${YAML_OUTPUT_DIR:-}" ]]; then
  readonly YAML_OUTPUT_DIR="$(mktemp -d)"
fi
rm -fr ${YAML_OUTPUT_DIR}/*.yaml

# Generated Knative component YAML files
readonly SERVING_CORE_YAML=${YAML_OUTPUT_DIR}/serving-core.yaml
readonly SERVING_DEFAULT_DOMAIN_YAML=${YAML_OUTPUT_DIR}/serving-default-domain.yaml
readonly SERVING_STORAGE_VERSION_MIGRATE_YAML=${YAML_OUTPUT_DIR}/serving-storage-version-migration.yaml
readonly SERVING_HPA_YAML=${YAML_OUTPUT_DIR}/serving-hpa.yaml
readonly SERVING_DOMAINMAPPING_YAML=${YAML_OUTPUT_DIR}/serving-domainmapping.yaml
readonly SERVING_DOMAINMAPPING_CRD_YAML=${YAML_OUTPUT_DIR}/serving-domainmapping-crds.yaml
readonly SERVING_CRD_YAML=${YAML_OUTPUT_DIR}/serving-crds.yaml
readonly SERVING_NSCERT_YAML=${YAML_OUTPUT_DIR}/serving-nscert.yaml
readonly SERVING_POST_INSTALL_JOBS_YAML=${YAML_OUTPUT_DIR}/serving-post-install-jobs.yaml

declare -A CONSOLIDATED_ARTIFACTS
CONSOLIDATED_ARTIFACTS=(
  ["${SERVING_POST_INSTALL_JOBS_YAML}"]="${SERVING_STORAGE_VERSION_MIGRATE_YAML}"
)
readonly CONSOLIDATED_ARTIFACTS

# Flags for all ko commands
KO_YAML_FLAGS="-P"
[[ "${KO_DOCKER_REPO}" != gcr.io/* ]] && KO_YAML_FLAGS=""

if [[ "${KO_FLAGS}" != *"--platform"* ]]; then
  KO_YAML_FLAGS="${KO_YAML_FLAGS} --platform=all"
fi

readonly KO_YAML_FLAGS="${KO_YAML_FLAGS} ${KO_FLAGS}"

if [[ -n "${TAG}" ]]; then
  LABEL_YAML_CMD=(sed -e "s|serving.knative.dev/release: devel|serving.knative.dev/release: \"${TAG}\"|")
else
  LABEL_YAML_CMD=(cat)
fi

: ${KO_DOCKER_REPO:="ko.local"}
export KO_DOCKER_REPO

cd "${YAML_REPO_ROOT}"

echo "Building Knative Serving"
ko resolve ${KO_YAML_FLAGS} -R -f config/core/ | "${LABEL_YAML_CMD[@]}" > "${SERVING_CORE_YAML}"

ko resolve ${KO_YAML_FLAGS} -f config/post-install/default-domain.yaml | "${LABEL_YAML_CMD[@]}" > "${SERVING_DEFAULT_DOMAIN_YAML}"

ko resolve ${KO_YAML_FLAGS} -f config/post-install/storage-version-migration.yaml | "${LABEL_YAML_CMD[@]}" > "${SERVING_STORAGE_VERSION_MIGRATE_YAML}"

# These don't have images, but ko will concatenate them for us.
ko resolve ${KO_YAML_FLAGS} -f config/core/300-resources/ -f config/core/300-imagecache.yaml | "${LABEL_YAML_CMD[@]}" > "${SERVING_CRD_YAML}"

# Create hpa-class autoscaling related yaml
ko resolve ${KO_YAML_FLAGS} -f config/hpa-autoscaling/ | "${LABEL_YAML_CMD[@]}" > "${SERVING_HPA_YAML}"

# Create domain mapping related yaml
ko resolve ${KO_YAML_FLAGS} -R -f config/domain-mapping/ | "${LABEL_YAML_CMD[@]}" > "${SERVING_DOMAINMAPPING_YAML}"
ko resolve ${KO_YAML_FLAGS} -f config/domain-mapping/300-resources/ | "${LABEL_YAML_CMD[@]}" > "${SERVING_DOMAINMAPPING_CRD_YAML}"

# Create nscert related yaml
ko resolve ${KO_YAML_FLAGS} -f config/namespace-wildcard-certs | "${LABEL_YAML_CMD[@]}" > "${SERVING_NSCERT_YAML}"

# By putting the list of files used to create serving-upgrade.yaml
# people can choose to exclude certain ones via 'grep' but still keep in-sync
# with the complete list if things change in the future
for artifact in "${!CONSOLIDATED_ARTIFACTS[@]}"; do
  echo "Assembling Knative Serving - ${artifact}"
  echo "" > ${artifact}
  for component in ${CONSOLIDATED_ARTIFACTS[${artifact}]}; do
    echo "---" >> ${artifact}
    echo "# ${component}" >> ${artifact}
    cat ${component} >> ${artifact}
  done
done

echo "All manifests generated"

# List generated YAML files, with serving-core.yaml first.

cat << EOF > ${YAML_LIST_FILE}
${SERVING_CORE_YAML}
${SERVING_DEFAULT_DOMAIN_YAML}
${SERVING_STORAGE_VERSION_MIGRATE_YAML}
${SERVING_POST_INSTALL_JOBS_YAML}
${SERVING_HPA_YAML}
${SERVING_DOMAINMAPPING_YAML}
${SERVING_DOMAINMAPPING_CRD_YAML}
${SERVING_CRD_YAML}
${SERVING_NSCERT_YAML}
EOF
