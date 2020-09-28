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

set -o errexit
set -o pipefail

# Install https://github.com/kubernetes/release/tree/master/cmd/release-notes
# Last version  I tested with was 0ffc7ad

readonly ROOT_DIR=$(dirname $0)/..

if [[ -z "${GITHUB_TOKEN}" ]]; then
  echo "Expected GITHUB_TOKEN to be set"
  exit 1
fi


start_sha() {
  local semver=$(git describe --match "v[0-9]*" --abbrev=0)
  git rev-list -n 1 "${semver}"
}


out=$(mktemp)

release-notes \
    --org knative \
    --repo serving \
    --required-author '' \
    --output "$out" \
    --start-sha "$(start_sha)" \
    --end-sha "$(git rev-list -n 1 HEAD)"

cat "$out"
