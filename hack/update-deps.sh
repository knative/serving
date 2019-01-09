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
set -o nounset
set -o pipefail

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/library.sh

# Remove two type of invalid symlinks
# 1. Broken symlinks
# 2. Symlinks pointing outside of the source tree
# We use recursion to find the canonical path of a symlink
# because both `realpath` and `readlink -m` are not available on Mac.
function remove_invalid_symlink() {
  cd ${REPO_ROOT_DIR}

  local target_file=$1
  if [[ ! -e ${target_file} ]] ; then
    echo "Removing broken symlink:" $1
    unlink ${REPO_ROOT_DIR}/$1
    return
  fi

  cd $(dirname ${target_file})
  target_file=$(basename ${target_file})
  # Iterate down a [possible] chain of symlinks
  while [[ -L ${target_file} ]] ; do
    target_file=$(readlink ${target_file})
    cd $(dirname ${target_file})
    target_file=$(basename ${target_file})
  done

  # Compute the canonicalized name by finding the physical path
  # for the directory we're in and appending the target file.
  local phys_dir=`pwd -P`
  target_file=${phys_dir}/${target_file}

  if [[ ${target_file} != *"github.com/knative/serving"* ]]; then
    echo "Removing symlink outside the source tree: " $1 "->" ${target_file}
    unlink ${REPO_ROOT_DIR}/$1
  fi
}



cd ${REPO_ROOT_DIR}

# Ensure we have everything we need under vendor/
dep ensure

rm -rf $(find vendor/ -name 'OWNERS')
rm -rf $(find vendor/ -name '*_test.go')

update_licenses third_party/VENDOR-LICENSE "./cmd/*"

# Patch the Kubernetes dynamic client to fix listing. This patch is from
# https://github.com/kubernetes/kubernetes/pull/68552/files, which is a
# cherrypick of #66078.  Remove this once that reaches a client version
# we have pulled in.
git apply ${REPO_ROOT_DIR}/hack/66078.patch

# Remove all invalid symlinks under ./vendor
for l in $(find vendor/ -type l); do
  remove_invalid_symlink $l
done
