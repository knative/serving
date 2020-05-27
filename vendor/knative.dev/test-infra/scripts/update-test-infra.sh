#!/usr/bin/env bash

# Copyright 2020 The Knative Authors
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

set -e

# This script updates test-infra scripts in-repo.
# Run it to update (usually from hack/update-deps.sh) the current scripts.
# Scripts are installed to REPO_ROOT/scripts/test-infra

# The following arguments are accepted:
# --update
#  Do the update
# --ref X
#  Defines which ref (branch, tag, commit) of test-infra to get scripts from; defaults to master
# --first-time
#  Run this script from your repo root directory to install scripts for the first time
#  Will also sed -i non-vendor scripts in the current repo to point to new path
# TODO: --verify
#  Verify the contents of scripts/test-infra match the contents from commit sha in scripts/test-infra/COMMIT
#  One can verify manually by running the script with '--ref $(cat scripts/test-infra/COMMIT)' and ensuring no files are staged

declare -i FIRST_TIME_SETUP=0
declare -i DO_UPDATE=0
declare SCRIPTS_REF=master

while [[ $# -ne 0 ]]; do
  parameter="$1"
  case ${parameter} in
    --ref)
      shift
      SCRIPTS_REF="$1"
      ;;
    --first-time)
      FIRST_TIME_SETUP=1
      ;;
    --update)
      DO_UPDATE=1
      ;;
    *)
      echo "unknown option ${parameter}"
      exit 1
      ;;
  esac
  shift
done

function do_read_tree() {
    mkdir -p scripts/test-infra
    git read-tree --prefix=scripts/test-infra -u "test-infra/${SCRIPTS_REF}:scripts"
    git show-ref -s -- "refs/remotes/test-infra/${SCRIPTS_REF}" > scripts/test-infra/COMMIT
    git add scripts/test-infra/COMMIT
    echo "test-infra scripts installed to scripts/test-infra from branch ${SCRIPTS_REF}"
}

function run() {
  if (( FIRST_TIME_SETUP )); then
    if [[ ! -d .git ]]; then
      echo "I don't believe you are in a repo root; exiting"
      exit 5
    fi
    git remote add test-infra https://github.com/knative/test-infra.git || echo "test-infra remote already set; not changing"
    git fetch test-infra "${SCRIPTS_REF}"
    do_read_tree
    echo "Attempting to point all scripts to use this new path"
    grep -RiIl vendor/knative.dev/test-infra | grep -v ^vendor | grep -v ^scripts/test-infra | xargs sed -i 's+vendor/knative.dev/test-infra/scripts+scripts/test-infra+'
  elif (( DO_UPDATE )); then
    pushd "$(dirname "${BASH_SOURCE[0]}")/../.."
    trap popd EXIT

    git remote add test-infra https://github.com/knative/test-infra.git || true
    git fetch test-infra "${SCRIPTS_REF}"
    git rm -fr scripts/test-infra
    rm -fR scripts/test-infra
    do_read_tree
  fi
}

run
