#!/bin/bash

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
set -o nounset
set -o pipefail

REPO_ROOT_DIR=$(dirname $0)/..
cd ${REPO_ROOT_DIR}

# TODO: verify the go version can satisfy the requirement.
if [[ -z "$(which gofmt)" ]]; then
  echo "Can't find 'gofmt' in PATH, please install and retry."
  exit 1
fi
gofmt=$(which gofmt)

find_files() {
  find . -not \( \
      \( \
        -wholename '*/third_party/*' \
        -o -wholename '*/vendor/*' \
      \) -prune \
    \) -name '*.go'
}

GOFMT="${gofmt} -s -w"
find_files | xargs $GOFMT
