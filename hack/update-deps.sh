#!/bin/bash

# Copyright 2017 The Kubernetes Authors.
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

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..

pushd ${SCRIPT_ROOT}
trap popd EXIT

# Ensure we have everything we need under vendor/
dep ensure
dep prune

# Make sure that BUILD files are up to date (the above removes them).
bazel run //:gazelle -- -proto=disable

# Rewrite the code-generator imports, which we pull in through WORKSPACE,
# since it's hard to vendor.
sed -i 's|//vendor/k8s.io/code-generator/|@io_k8s_code_generator//|g' \
    $(find ${SCRIPT_ROOT}/vendor -type f -name '*' | xargs grep k8s.io/code-generator | cut -d':' -f 1 | uniq)

# Fix up a case in k8s' client-go where non-testdata relies on files
# in testdata (and so breaks after pruning).
sed -i 's|.*".*dontUseThisKey.pem",||g' vendor/k8s.io/client-go/util/cert/BUILD
