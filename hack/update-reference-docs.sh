#!/usr/bin/env bash

# Copyright 2021 The Knative Authors
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

export GO111MODULE=on

if [ -z "${GOPATH:-}" ]; then
  export GOPATH=$(go env GOPATH)
fi

source $(dirname $0)/../vendor/knative.dev/hack/library.sh

(
  cd ${REPO_ROOT_DIR}
  go run "${REPO_ROOT_DIR}/vendor/github.com/ahmetb/gen-crd-api-reference-docs" \
    -out-file "docs/serving-api.md" \
    -api-dir "knative.dev/serving/pkg/apis" \
    -template-dir "${REPO_ROOT_DIR}/vendor/github.com/ahmetb/gen-crd-api-reference-docs/template" \
    -config "${REPO_ROOT_DIR}/hack/reference-docs-gen-config.json"
)
