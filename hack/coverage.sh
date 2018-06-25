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

#TODO delete this file after all prow jobs points to project-coverage.sh

set -o errexit

if [[ $# -eq 0 ]] ; then
  echo "using commit id as profile object name"
  OBJECT_FILE_NAME=$PULL_PULL_SHA
else 
  echo "using pre-defined profile object name: " + $1
  OBJECT_FILE_NAME=$1
fi

SERVING_ROOT=$(dirname ${BASH_SOURCE})/..

cd ${SERVING_ROOT}/pkg

# Generate the coverage profile for all tests, and store it in the GCS bucket.
go test ./... -coverprofile coverage_profile.txt
gsutil cp -a public-read coverage_profile.txt gs://gke-prow/pr-logs/directory/knative-coverage/profiles/$OBJECT_FILE_NAME
