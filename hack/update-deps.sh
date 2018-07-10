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

SERVING_ROOT=$(dirname ${BASH_SOURCE})/..

pushd ${SERVING_ROOT}
trap popd EXIT

# Ensure we have everything we need under vendor/
dep ensure

# Patch the Kubernetes client to fix panics in fake watches. This patch is from
# https://github.com/kubernetes/kubernetes/pull/61195 and can be removed once
# that PR makes it here.
git apply --exclude='*_test.go' $SERVING_ROOT/hack/61195.patch

rm -rf $(find vendor/ -name 'OWNERS')
rm -rf $(find vendor/ -name '*_test.go')
