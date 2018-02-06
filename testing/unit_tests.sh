#!/bin/bash

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

# Fail on any error, display commands being run.
set -ex

# Clean up any leftover configuration
readonly DOCKER_REPO_OVERRIDE=dummy_docker
readonly K8S_CLUSTER_OVERRIDE=dummy_k8s_cluster
bazel clean

set +e
bazel test //pkg/... --test_output=errors
RESULT=$?
set -e

exit $RESULT
