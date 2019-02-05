#!/usr/bin/env bash
#
# Copyright 2018 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit

function upload_test_images() {
  echo ">> Publishing test images"
  local image_dirs="$(find $(dirname $0)/test_images -mindepth 1 -maxdepth 1 -type d)"
  local docker_tag=$1
  local tag_option=""
  if [ -n "${docker_tag}" ]; then
    tag_option="--tags $docker_tag"
  fi

  for image_dir in ${image_dirs}; do
      local image_name="$(basename ${image_dir})"
      local image="github.com/knative/serving/test/test_images/${image_name}"

      ko publish -B ${tag_option} "${image}"
  done
}

: ${DOCKER_REPO_OVERRIDE:?"You must set 'DOCKER_REPO_OVERRIDE', see DEVELOPMENT.md"}

export KO_DOCKER_REPO=${DOCKER_REPO_OVERRIDE}

upload_test_images $@
