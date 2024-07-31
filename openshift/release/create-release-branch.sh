#!/usr/bin/env bash

# Usage: create-release-branch.sh v0.4.1 release-0.4

set -e # Exit immediately on error.

release=$1
target=$2

# Fetch the latest tags and checkout a new branch from the wanted tag.
git fetch upstream --tags
git checkout -b "$target" "$release"

# Remove GH Action hooks from upstream
rm -rf .github/workflows
git commit -sm ":fire: remove unneeded workflows" .github/

# Copy the openshift extra files from the OPENSHIFT/main branch.
git fetch openshift main
git checkout openshift/main -- .github/workflows openshift OWNERS_ALIASES OWNERS Makefile

make generate-dockerfiles
make RELEASE="$release" generate-release
git add .github/workflows openshift OWNERS_ALIASES OWNERS Makefile
git commit -m "Add openshift specific files."

# Apply patches .
PATCH_DIR="openshift/patches"
# Use release-specific patch dir if exists
if [ -d "openshift/patches-${release}" ]; then
    PATCH_DIR="openshift/patches-${release}"
    # Update the nightly test images to actual versioned images
    sed -i "s/knative-nightly:knative/knative-${release}:knative/g" "${PATCH_DIR}"/*.patch
fi
git apply "$PATCH_DIR"/*
make RELEASE="$release" generate-release
git add .
git commit -am ":fire: Apply carried patches."
