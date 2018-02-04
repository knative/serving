#!/bin/bash

# Copyright 2018 Google, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eu

# This allows RUNFILES to be declared outside the script it you want.
# RUNFILES for test is the directory of the script.
RUNFILES=${RUNFILES:-$($(cd $(dirname ${BASH_SOURCE[0]})); pwd)}

function test_build {
  OUR_PATH="${RUNFILES}/pkg/apis/cloudbuild/v1alpha1/build_types.go"
  THEIR_PATH="${RUNFILES}/external/buildcrd/pkg/apis/cloudbuild/v1alpha1/build_types.go"

  if [ "$(cat ${OUR_PATH})" != "$(cat ${THEIR_PATH})" ]; then
    echo ERROR: build_types.go does not match the version imported by WORKSPACE.
    diff "${OUR_PATH}" "${THEIR_PATH}"
    exit 1
  fi
}

function test_build_template {
  OUR_PATH="${RUNFILES}/pkg/apis/cloudbuild/v1alpha1/build_template_types.go"
  THEIR_PATH="${RUNFILES}/external/buildcrd/pkg/apis/cloudbuild/v1alpha1/build_template_types.go"

  if [ "$(cat ${OUR_PATH})" != "$(cat ${THEIR_PATH})" ]; then
    echo ERROR: build_template_types.go does not match the version imported by WORKSPACE.
    diff "${OUR_PATH}" "${THEIR_PATH}"
    exit 1
  fi
}

test_build
test_build_template
