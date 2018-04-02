#!/bin/bash

# Copyright 2018 Google, Inc. All rights reserved.
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

ELAFROS_ROOT=$(dirname ${BASH_SOURCE})/..

cd ${ELAFROS_ROOT}/pkg

# Generate the coverage profile for all tests, and store it in the GCS bucket.
# TODO(steuhs): get PR number and use that as the file name
go test ./... -coverprofile coverage_profile.txt
gsutil cp -a project-private coverage_profile.txt gs://gke-prow/pr-logs/directory/elafros-coverage/profiles/$PULL_PULL_SHA
