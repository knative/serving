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

COVERAGE_FILE_NAME="coverage_profile.txt"
HEAD_FILE_NAME="head.txt"
ELAFROS_ROOT=$(dirname ${BASH_SOURCE})/..

if [[ -v PULL_PULL_SHA ]]; then 
  echo "Process pull build..."
  object_file_name=$PULL_PULL_SHA
  pr=$PULL_NUMBER
else
  # post-submits (merging into master) has no PULL_PULL_SHA, 
  # the commit id is stored in PULL_BASE_SHA instead
  echo "Processing master commit..."
  if [[ -v PULL_BASE_SHA ]]; then
    object_file_name=$PULL_BASE_SHA
    pr=master
  else
    echo "Error: None of PULL_PULL_SHA and PULL_BASE_SHA is set"
    exit 1
  fi
fi

case $pr in 
  master) folder="master";;
  *) folder="pulls/$pr";;
esac

gcs_pr_dir="gs://gke-prow/pr-logs/coverage/elafros-elafros/${folder}/"
gcs_profile_dir=${gcs_pr_dir}profiles/
gcs_profile_path=${gcs_profile_dir}${object_file_name}


cd ${ELAFROS_ROOT}/pkg

# Generate the coverage profile for all tests, and store it in the GCS bucket.
go test ./... -coverprofile $COVERAGE_FILE_NAME
gsutil cp -a public-read $COVERAGE_FILE_NAME $gcs_profile_path/$COVERAGE_FILE_NAME
echo "Test coverage profiling completed successfully"
rm $COVERAGE_FILE_NAME

# Record the commit id and master commit id of the latest build
commit_record="$PULL_PULL_SHA $PULL_BASE_SHA `date +%s%N`"
echo ${commit_record} > $HEAD_FILE_NAME
echo "Uploading head record to GCS..."
gsutil cp -a public-read $HEAD_FILE_NAME ${gcs_pr_dir}${HEAD_FILE_NAME}
echo "Finished uploading head record to GCS..."

# Append the commit id and master commit id of the latest build to the end of the history
hist_heads_path=${gcs_pr_dir}historical_heads

(gsutil -q stat $hist_heads_path) || is_hist_empty=1
if [ $is_hist_empty -eq 1 ]; then
  echo "Creating new history file"
  gsutil cp -a public-read $HEAD_FILE_NAME $hist_heads_path
else
  # download the existing version, update locally and then upload to refresh
  echo "Appending new record to history file"
  gsutil cp $hist_heads_path $HEAD_FILE_NAME
  echo ${commit_record} >> $HEAD_FILE_NAME
  gsutil cp -a public-read $HEAD_FILE_NAME $hist_heads_path
fi
rm $HEAD_FILE_NAME
echo "Coverage related GCS uploading completed"
