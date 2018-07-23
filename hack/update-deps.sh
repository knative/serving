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

# Load github.com/knative/test-infra/images/prow-tests/scripts/library.sh
[ -f /workspace/library.sh ] \
  && source /workspace/library.sh \
  || eval "$(docker run --entrypoint sh gcr.io/knative-tests/test-infra/prow-tests -c 'cat library.sh')"
[ -v KNATIVE_TEST_INFRA ] || exit 1

set -o errexit
set -o nounset
set -o pipefail

cd ${REPO_ROOT_DIR}

# Ensure we have everything we need under vendor/
dep ensure

# Patch the Kubernetes client to fix panics in fake watches. This patch is from
# https://github.com/kubernetes/kubernetes/pull/61195 and can be removed once
# that PR makes it here.
git apply --exclude='*_test.go' ${REPO_ROOT_DIR}/hack/61195.patch

rm -rf $(find vendor/ -name 'OWNERS')
rm -rf $(find vendor/ -name '*_test.go')

update_licenses third_party/VENDOR-LICENSE "./cmd/*"
