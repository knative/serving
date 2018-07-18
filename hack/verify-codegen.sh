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

source "$(dirname $(readlink -f ${BASH_SOURCE}))/../test/library.sh"

set -o errexit
set -o nounset
set -o pipefail

readonly TMP_DIFFROOT="$(mktemp -d -p ${SERVING_ROOT_DIR})"

cleanup() {
  rm -rf "${TMP_DIFFROOT}"
}

trap "cleanup" EXIT SIGINT

cleanup

# Save working tree state
mkdir -p "${TMP_DIFFROOT}/pkg"
cp -aR "${SERVING_ROOT_DIR}/Gopkg.lock" "${SERVING_ROOT_DIR}/pkg" "${SERVING_ROOT_DIR}/vendor" "${TMP_DIFFROOT}"

# We symlink a few testdata files from config, so copy it as well.
mkdir -p "${TMP_DIFFROOT}/config"
cp -a "${SERVING_ROOT_DIR}/config"/* "${TMP_DIFFROOT}/config"

# TODO(mattmoor): We should be able to rm -rf pkg/client/ and vendor/

"${SERVING_ROOT_DIR}/hack/update-codegen.sh"
echo "Diffing ${SERVING_ROOT_DIR} against freshly generated codegen"
ret=0
diff -Naupr "${SERVING_ROOT_DIR}/pkg" "${TMP_DIFFROOT}/pkg" || ret=$?

# Restore working tree state
rm -fr "${TMP_DIFFROOT}/config"
rm -fr "${SERVING_ROOT_DIR}/Gopkg.lock" "${SERVING_ROOT_DIR}/pkg" "${SERVING_ROOT_DIR}/vendor"
cp -aR "${TMP_DIFFROOT}"/* "${SERVING_ROOT_DIR}"

if [[ $ret -eq 0 ]]
then
  echo "${SERVING_ROOT_DIR} up to date."
else
  echo "ERROR: ${SERVING_ROOT_DIR} is out of date. Please run ./hack/update-codegen.sh"
  exit 1
fi
