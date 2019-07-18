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
source ${ROOT_DIR}/vendor/github.com/knative/test-infra/scripts/library.sh

set -o errexit
set -o nounset
set -o pipefail

cd ${ROOT_DIR}

# Ensure we have everything we need under vendor/
dep ensure

rm -rf $(find vendor/ -name 'OWNERS')
rm -rf $(find vendor/ -name '*_test.go')

update_licenses third_party/VENDOR-LICENSE "./cmd/*"

# Patch k8s.io/client-go/tools/cache and k8s.io/kbernetes/pkg/credentialprovider
# to make k8schain work with ECR. This is a workaround for:
# https://github.com/google/go-containerregistry/issues/355
#
# Once we're on 1.15 we can drop this patch, but we will have to be careful when
# bumping kubernetes dependencies.
#
# TODO(#4549): Drop this patch.
git apply ${REPO_ROOT_DIR}/hack/1996.patch

remove_broken_symlinks ./vendor
