#!/bin/bash
#
# Copyright 2018 Google LLC
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

set -ex

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

DOCKER_REPO_OVERRIDE=${DOCKER_REPO_OVERRIDE?"You must set `DOCKER_REPO_OVERRIDE`, see DEVELOPMENT.md"}
IMAGE_NAME="${DOCKER_REPO_OVERRIDE}/noodleburg"

for version in autoscale helloworld
do
    VERSIONED_NAME="${IMAGE_NAME}-${version}"
    docker build "$DIR/test_images_node/$version/" -f "$DIR/test_images_node/$version/Dockerfile" -t "$VERSIONED_NAME"
    docker push "$VERSIONED_NAME"
done
