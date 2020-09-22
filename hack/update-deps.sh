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

readonly ROOT_DIR=$(dirname $0)/..
source ${ROOT_DIR}/vendor/knative.dev/test-infra/scripts/library.sh

set -o errexit
set -o nounset
set -o pipefail

cd ${ROOT_DIR}

# We need these flags for things to work properly.
export GO111MODULE=on

# This controls the release branch we track.
VERSION="release-0.18"

# The list of dependencies that we track at HEAD and periodically
# float forward in this repository.
FLOATING_DEPS=(
  "knative.dev/test-infra@${VERSION}"
  "knative.dev/pkg@${VERSION}"
  "knative.dev/networking@${VERSION}"
  "knative.dev/caching@${VERSION}"
)

# Parse flags to determine any we should pass to dep.
GO_GET=0
while [[ $# -ne 0 ]]; do
  parameter=$1
  case ${parameter} in
    --upgrade) GO_GET=1 ;;
    *) abort "unknown option ${parameter}" ;;
  esac
  shift
done
readonly GO_GET

if (( GO_GET )); then
  go get -d ${FLOATING_DEPS[@]}
fi


# Prune modules.
go mod tidy
go mod vendor

echo "Removing unwanted vendor files"

# Remove unwanted vendor files
find vendor/ \( -name "OWNERS" -o -name "*_test.go" \) -print0 | xargs -0 rm -f

export GOFLAGS=-mod=vendor

echo "Updating licenses"
update_licenses third_party/VENDOR-LICENSE "./..."

echo "Removing broken symlinks"
remove_broken_symlinks ./vendor
