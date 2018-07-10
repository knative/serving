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

SERVING_ROOT=$(dirname "${BASH_SOURCE}")/..

DIFFROOT="${SERVING_ROOT}"
TMP_DIFFROOT="${SERVING_ROOT}/_tmp"
_tmp="${SERVING_ROOT}/_tmp"

cleanup() {
  rm -rf "${_tmp}"
}

trap "cleanup" EXIT SIGINT

cleanup

mkdir -p "${TMP_DIFFROOT}/pkg"
cp -a "${DIFFROOT}/pkg"/* "${TMP_DIFFROOT}/pkg"

# We symlink a few testdata files from config, so copy it as well.
mkdir -p "${TMP_DIFFROOT}/config"
cp -a "${DIFFROOT}/config"/* "${TMP_DIFFROOT}/config"

# TODO(mattmoor): We should be able to rm -rf pkg/client/ and vendor/

"${SERVING_ROOT}/hack/update-codegen.sh"
echo "Diffing ${DIFFROOT} against freshly generated codegen"
ret=0
diff -Naupr "${DIFFROOT}/pkg" "${TMP_DIFFROOT}/pkg" || ret=$?
cp -a "${TMP_DIFFROOT}/pkg"/* "${DIFFROOT}/pkg"
if [[ $ret -eq 0 ]]
then
  echo "${DIFFROOT} up to date."
else
  echo "ERROR: ${DIFFROOT} is out of date. Please run ./hack/update-codegen.sh"
  exit 1
fi
