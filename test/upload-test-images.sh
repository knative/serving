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

: ${DOCKER_REPO_OVERRIDE:?"You must set 'DOCKER_REPO_OVERRIDE', see DEVELOPMENT.md"}

export KO_DOCKER_REPO=${DOCKER_REPO_OVERRIDE}
IMAGE_DIRS="$(find $(dirname $0)/test_images -mindepth 1 -maxdepth 1 -type d)"
DOCKER_TAG=$1

for image_dir in ${IMAGE_DIRS}; do
  IMAGE="github.com/knative/serving/test/test_images/$(basename ${image_dir})"
  ko publish -P $IMAGE
  if [ -n "$DOCKER_TAG" ]; then
    IMAGE=$KO_DOCKER_REPO/$IMAGE
    DIGEST=$(docker images | grep $IMAGE | head -1 | awk '{print $2}')
    echo "Tagging $IMAGE:$DIGEST with $DOCKER_TAG"
    docker tag $IMAGE:$DIGEST $IMAGE:$DOCKER_TAG
  fi
done
