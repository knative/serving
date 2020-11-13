#!/usr/bin/env bash

# Copyright 2020 The Knative Authors
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
export K8S_VERSION="$1"

K8S_DEPS=(
  "k8s.io/api"
  "k8s.io/apiextensions-apiserver"
  "k8s.io/apimachinery"
  "k8s.io/code-generator"
  "k8s.io/client-go"
)


for dep in "${K8S_DEPS[@]}"
do
  go mod edit \
    -require="${dep}@${K8S_VERSION}" \
    -replace="${dep}=${dep}@${K8S_VERSION}"
done

./hack/update-deps.sh

