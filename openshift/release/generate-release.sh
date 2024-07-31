#!/usr/bin/env bash

root="$(dirname "${BASH_SOURCE[0]}")"

source $(dirname $0)/resolve.sh
# "VERSION" is used for the "app.kubernetes.io/version" label.
VERSION=$1

readonly YAML_OUTPUT_DIR="openshift/release/artifacts/"

# Clean up
rm -rf "$YAML_OUTPUT_DIR"
mkdir -p "$YAML_OUTPUT_DIR"

readonly SERVING_CRD_YAML=${YAML_OUTPUT_DIR}/serving-crds.yaml
readonly SERVING_CORE_YAML=${YAML_OUTPUT_DIR}/serving-core.yaml
readonly SERVING_HPA_YAML=${YAML_OUTPUT_DIR}/serving-hpa.yaml
readonly SERVING_POST_INSTALL_JOBS_YAML=${YAML_OUTPUT_DIR}/serving-post-install-jobs.yaml

if [[ "$VERSION" == "ci" ]]; then
  # Do not use devel as operator checks the version.
  VERSION="release-v1.2"
elif [[ "$VERSION" =~ "knative-" ]]; then
  # openshift/release/create-release-branch.sh
  # Drop the "knative-" prefix and micro version, which is used in create-release-branch.sh.
  # e.g. knative-v1.7.0 => release-v1.7
  VERSION="release-"${VERSION#"knative-"}
  VERSION="${VERSION%.*}"
fi

# Generate Knative component YAML files
resolve_resources "config/core/300-resources/ config/core/300-imagecache.yaml" "$SERVING_CRD_YAML"                    "$VERSION"
resolve_resources "config/core/"                                               "$SERVING_CORE_YAML"                   "$VERSION"
resolve_resources "config/hpa-autoscaling/"                                    "$SERVING_HPA_YAML"                    "$VERSION"
resolve_resources "config/post-install/storage-version-migration.yaml"         "$SERVING_POST_INSTALL_JOBS_YAML"      "$VERSION"
