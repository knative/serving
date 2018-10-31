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

# Fake we're in a Prow job, if running locally.
[[ -z "${PROW_JOB_ID:-}" ]] && PROW_JOB_ID=123

source $(dirname $0)/../scripts/library.sh

set -e

function test_report() {
  local REPORT="$(mktemp)"
  report_go_test -run $1 ./test > ${REPORT} || true
  cat ${REPORT}
  grep "$2" ${REPORT} > /dev/null
  grep "Done parsing 1 tests" ${REPORT} > /dev/null
}

header "Testing report_go_test"

subheader "Test pass"
test_report TestSucceeds "^- TestSucceeds :PASS:"

subheader "Test fails with fatal"
test_report TestFailsWithFatal "^- TestFailsWithFatal :FAIL:"

subheader "Test fails with SIGQUIT"
test_report TestFailsWithSigQuit "^- TestFailsWithSigQuit :FAIL:"

header "All tests passed"
