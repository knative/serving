#!/bin/bash
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

: ${1:?"Pass the directory with the test images as argument"}
: ${DOCKER_REPO_OVERRIDE:?"You must set 'DOCKER_REPO_OVERRIDE', see DEVELOPMENT.md"}

DOCKER_FILES="$(ls -1 $1/*/Dockerfile)"
: ${DOCKER_FILES:?"No subdirectories with Dockerfile files found in $1"}

for docker_file in ${DOCKER_FILES}; do
  image_dir="$(dirname ${docker_file})"
  versioned_name="${DOCKER_REPO_OVERRIDE}/$(basename ${image_dir})"
  docker build "${image_dir}" -f "${docker_file}" -t "${versioned_name}"
  docker push "${versioned_name}"
done
