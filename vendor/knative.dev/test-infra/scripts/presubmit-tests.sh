#!/usr/bin/env bash

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

# This is a helper script for Knative presubmit test scripts.
# See README.md for instructions on how to use it.

source $(dirname ${BASH_SOURCE})/library.sh

# Custom configuration of presubmit tests
readonly DISABLE_MD_LINTING=${DISABLE_MD_LINTING:-0}
readonly DISABLE_MD_LINK_CHECK=${DISABLE_MD_LINK_CHECK:-0}
readonly PRESUBMIT_TEST_FAIL_FAST=${PRESUBMIT_TEST_FAIL_FAST:-0}

# Extensions or file patterns that don't require presubmit tests.
readonly NO_PRESUBMIT_FILES=(\.png \.gitignore \.gitattributes ^OWNERS ^OWNERS_ALIASES ^AUTHORS)

# Flag if this is a presubmit run or not.
(( IS_PROW )) && [[ -n "${PULL_PULL_SHA}" ]] && IS_PRESUBMIT=1 || IS_PRESUBMIT=0
readonly IS_PRESUBMIT

# List of changed files on presubmit, LF separated.
CHANGED_FILES=""

# Flags that this PR is exempt of presubmit tests.
IS_PRESUBMIT_EXEMPT_PR=0

# Flags that this PR contains only changes to documentation.
IS_DOCUMENTATION_PR=0

# Returns true if PR only contains the given file regexes.
# Parameters: $1 - file regexes, space separated.
function pr_only_contains() {
  [[ -z "$(echo "${CHANGED_FILES}" | grep -v "\(${1// /\\|}\)$")" ]]
}

# Initialize flags and context for presubmit tests:
# CHANGED_FILES, IS_PRESUBMIT_EXEMPT_PR and IS_DOCUMENTATION_PR.
function initialize_environment() {
  CHANGED_FILES=""
  IS_PRESUBMIT_EXEMPT_PR=0
  IS_DOCUMENTATION_PR=0
  (( ! IS_PRESUBMIT )) && return
  CHANGED_FILES="$(list_changed_files)"
  if [[ -n "${CHANGED_FILES}" ]]; then
    echo -e "Changed files in commit ${PULL_PULL_SHA}:\n${CHANGED_FILES}"
    local no_presubmit_files="${NO_PRESUBMIT_FILES[*]}"
    pr_only_contains "${no_presubmit_files}" && IS_PRESUBMIT_EXEMPT_PR=1
    # A documentation PR must contain markdown files
    if pr_only_contains "\.md ${no_presubmit_files}"; then
      [[ -n "$(echo "${CHANGED_FILES}" | grep '\.md')" ]] && IS_DOCUMENTATION_PR=1
    fi
  else
    header "NO CHANGED FILES REPORTED, ASSUMING IT'S AN ERROR AND RUNNING TESTS ANYWAY"
  fi
  readonly CHANGED_FILES
  readonly IS_DOCUMENTATION_PR
  readonly IS_PRESUBMIT_EXEMPT_PR
}

# Display a pass/fail banner for a test group.
# Parameters: $1 - test group name (e.g., build)
#             $2 - result (0=passed, 1=failed)
function results_banner() {
  local result
  [[ $2 -eq 0 ]] && result="PASSED" || result="FAILED"
  header "$1 tests ${result}"
}

# Run build tests. If there's no `build_tests` function, run the default
# build test runner.
function run_build_tests() {
  (( ! RUN_BUILD_TESTS )) && return 0
  header "Running build tests"
  local failed=0
  # Run pre-build tests, if any
  if function_exists pre_build_tests; then
    pre_build_tests || { failed=1; step_failed "pre_build_tests"; }
  fi
  # Don't run build tests if pre-build tests failed
  if (( ! failed )); then
    if function_exists build_tests; then
      build_tests || { failed=1; step_failed "build_tests"; }
    else
      default_build_test_runner || { failed=1; step_failed "default_build_test_runner"; }
    fi
  fi
  # Don't run post-build tests if pre/build tests failed
  if (( ! failed )) && function_exists post_build_tests; then
    post_build_tests || { failed=1; step_failed "post_build_tests"; }
  fi
  results_banner "Build" ${failed}
  return ${failed}
}

# Run a build test and report its output as the failure if it fails.
# Parameters: $1 - report name.
#             $2... - command (test) to run.
function report_build_test() {
  local report="$(mktemp)"
  local report_name="$1"
  shift
  local errors=""
  capture_output "${report}" "$@" || errors="$(cat ${report})"
  create_junit_xml _build_tests "${report_name}" "${errors}"
  [[ -z "${errors}" ]]
}

# Perform markdown build tests if necessary, unless disabled.
function markdown_build_tests() {
  (( DISABLE_MD_LINTING && DISABLE_MD_LINK_CHECK )) && return 0
  # Get changed markdown files (ignore /vendor, github templates, and deleted files)
  local mdfiles=""
  for file in $(echo "${CHANGED_FILES}" | grep \\.md$ | grep -v ^vendor/ | grep -v ^.github/); do
    [[ -f "${file}" ]] && mdfiles="${mdfiles} ${file}"
  done
  [[ -z "${mdfiles}" ]] && return 0
  local failed=0
  if (( ! DISABLE_MD_LINTING )); then
    subheader "Linting the markdown files"
    report_build_test Markdown_Lint lint_markdown ${mdfiles} || failed=1
  fi
  if (( ! DISABLE_MD_LINK_CHECK )); then
    subheader "Checking links in the markdown files"
    report_build_test Markdown_Link check_links_in_markdown ${mdfiles} || failed=1
  fi
  return ${failed}
}

# Default build test runner that:
# * check markdown files
# * run `/hack/verify-codegen.sh` (if it exists)
# * `go build` on the entire repo
# * check licenses in all go packages
function default_build_test_runner() {
  local failed=0
  # Perform markdown build checks
  markdown_build_tests || failed=1
  # Run verify-codegen check
  if [[ -f ./hack/verify-codegen.sh ]]; then
    subheader "Checking autogenerated code is up-to-date"
    report_build_test Verify_CodeGen ./hack/verify-codegen.sh || failed=1
  fi
  # For documentation PRs, just check the md files and run
  # verify-codegen (as md files can be auto-generated in some repos).
  (( IS_DOCUMENTATION_PR )) && return ${failed}
  # Don't merge these two lines, or return code will always be 0.
  local go_pkg_dirs
  go_pkg_dirs="$(go list ./...)" || return 1
  # Skip build test if there is no go code
  [[ -z "${go_pkg_dirs}" ]] && return ${failed}
  # Ensure all the code builds
  subheader "Checking that go code builds"
  local report="$(mktemp)"
  local errors_go1=""
  local errors_go2=""
  if ! capture_output "${report}" go build -v ./... ; then
    failed=1
    # Consider an error message everything that's not a package name.
    errors_go1="$(grep -v '^\(github\.com\|knative\.dev\)/' "${report}" | sort | uniq)"
  fi
  # Get all build tags in go code (ignore /vendor, /hack and /third_party)
  local tags="$(grep -r '// +build' . \
    | grep -v '^./vendor/' | grep -v '^./hack/' | grep -v '^./third_party' \
    | cut -f3 -d' ' | sort | uniq | tr '\n' ' ')"
  local tagged_pkgs="$(grep -r '// +build' . \
    | grep -v '^./vendor/' | grep -v '^./hack/' | grep -v '^./third_party' \
    | grep ":// +build " | cut -f1 -d: | xargs dirname \
    | sort | uniq | tr '\n' ' ')"
  for pkg in ${tagged_pkgs}; do
    # `go test -c` lets us compile the tests but do not run them.
    if ! capture_output "${report}" go test -c -tags="${tags}" ${pkg} ; then
      failed=1
      # Consider an error message everything that's not a successful test result.
      errors_go2+="$(grep -v '^\(ok\|\?\)\s\+\(github\.com\|knative\.dev\)/' "${report}")"
    fi
    # Remove unused generated binary, if any.
    rm -f e2e.test
  done

  local errors_go="$(echo -e "${errors_go1}\n${errors_go2}" | uniq)"
  create_junit_xml _build_tests Build_Go "${errors_go}"
  # Check that we don't have any forbidden licenses in our images.
  subheader "Checking for forbidden licenses"
  report_build_test Check_Licenses check_licenses || failed=1
  return ${failed}
}

# Run unit tests. If there's no `unit_tests` function, run the default
# unit test runner.
function run_unit_tests() {
  (( ! RUN_UNIT_TESTS )) && return 0
  if (( IS_DOCUMENTATION_PR )); then
    header "Documentation only PR, skipping unit tests"
    return 0
  fi
  header "Running unit tests"
  local failed=0
  # Run pre-unit tests, if any
  if function_exists pre_unit_tests; then
    pre_unit_tests || { failed=1; step_failed "pre_unit_tests"; }
  fi
  # Don't run unit tests if pre-unit tests failed
  if (( ! failed )); then
    if function_exists unit_tests; then
      unit_tests || { failed=1; step_failed "unit_tests"; }
    else
      default_unit_test_runner || { failed=1; step_failed "default_unit_test_runner"; }
    fi
  fi
  # Don't run post-unit tests if pre/unit tests failed
  if (( ! failed )) && function_exists post_unit_tests; then
    post_unit_tests || { failed=1; step_failed "post_unit_tests"; }
  fi
  results_banner "Unit" ${failed}
  return ${failed}
}

# Default unit test runner that runs all go tests in the repo.
function default_unit_test_runner() {
  report_go_test -race ./...
}

# Run integration tests. If there's no `integration_tests` function, run the
# default integration test runner.
function run_integration_tests() {
  # Don't run integration tests if not requested OR on documentation PRs
  (( ! RUN_INTEGRATION_TESTS )) && return 0
  if (( IS_DOCUMENTATION_PR )); then
    header "Documentation only PR, skipping integration tests"
    return 0
  fi
  header "Running integration tests"
  local failed=0
  # Run pre-integration tests, if any
  if function_exists pre_integration_tests; then
    pre_integration_tests || { failed=1; step_failed "pre_integration_tests"; }
  fi
  # Don't run integration tests if pre-integration tests failed
  if (( ! failed )); then
    if function_exists integration_tests; then
      integration_tests || { failed=1; step_failed "integration_tests"; }
    else
      default_integration_test_runner || { failed=1; step_failed "default_integration_test_runner"; }
    fi
  fi
  # Don't run integration tests if pre/integration tests failed
  if (( ! failed )) && function_exists post_integration_tests; then
    post_integration_tests || { failed=1; step_failed "post_integration_tests"; }
  fi
  results_banner "Integration" ${failed}
  return ${failed}
}

# Default integration test runner that runs all `test/e2e-*tests.sh`.
function default_integration_test_runner() {
  local options=""
  local failed=0
  for e2e_test in $(find test/ -name e2e-*tests.sh); do
    echo "Running integration test ${e2e_test}"
    if ! ${e2e_test} ${options}; then
      failed=1
      step_failed "${e2e_test} ${options}"
    fi
  done
  return ${failed}
}

# Options set by command-line flags.
RUN_BUILD_TESTS=0
RUN_UNIT_TESTS=0
RUN_INTEGRATION_TESTS=0

# Process flags and run tests accordingly.
function main() {
  initialize_environment
  if (( IS_PRESUBMIT_EXEMPT_PR )) && (( ! IS_DOCUMENTATION_PR )); then
    header "Commit only contains changes that don't require tests, skipping"
    exit 0
  fi

  # Show the version of the tools we're using
  if (( IS_PROW )); then
    # Disable gcloud update notifications
    gcloud config set component_manager/disable_update_check true
    header "Current test setup"
    echo ">> gcloud SDK version"
    gcloud version
    echo ">> kubectl version"
    kubectl version --client
    echo ">> go version"
    go version
    echo ">> go env"
    go env
    echo ">> python3 version"
    python3 --version
    echo ">> git version"
    git version
    echo ">> ko version"
    [[ -f /ko_version ]] && cat /ko_version || echo "unknown"
    echo ">> bazel version"
    [[ -f /bazel_version ]] && cat /bazel_version || echo "unknown"
    if [[ "${DOCKER_IN_DOCKER_ENABLED}" == "true" ]]; then
      echo ">> docker version"
      docker version
    fi
    # node/pod names are important for debugging purposes, but they are missing
    # after migrating from bootstrap to podutil.
    # Report it here with the same logic as in bootstrap until it is fixed.
    # (https://github.com/kubernetes/test-infra/blob/09bd4c6709dc64308406443f8996f90cf3b40ed1/jenkins/bootstrap.py#L588)
    # TODO(chaodaiG): follow up on https://github.com/kubernetes/test-infra/blob/0fabd2ea816daa8c15d410c77a0c93c0550b283f/prow/initupload/run.go#L49
    echo ">> node name"
    echo "$(curl -H "Metadata-Flavor: Google" 'http://169.254.169.254/computeMetadata/v1/instance/name' 2> /dev/null)"
    echo ">> pod name"
    echo ${HOSTNAME}
  fi

  [[ -z $1 ]] && set -- "--all-tests"

  local TESTS_TO_RUN=()

  while [[ $# -ne 0 ]]; do
    local parameter=$1
    case ${parameter} in
      --build-tests) RUN_BUILD_TESTS=1 ;;
      --unit-tests) RUN_UNIT_TESTS=1 ;;
      --integration-tests) RUN_INTEGRATION_TESTS=1 ;;
      --all-tests)
        RUN_BUILD_TESTS=1
        RUN_UNIT_TESTS=1
        RUN_INTEGRATION_TESTS=1
        ;;
      --run-test)
        shift
        [[ $# -ge 1 ]] || abort "missing executable after --run-test"
        TESTS_TO_RUN+=("$1")
        ;;
      *) abort "error: unknown option ${parameter}" ;;
    esac
    shift
  done

  readonly RUN_BUILD_TESTS
  readonly RUN_UNIT_TESTS
  readonly RUN_INTEGRATION_TESTS
  readonly TESTS_TO_RUN

  cd ${REPO_ROOT_DIR}

  # Tests to be performed, in the right order if --all-tests is passed.

  local failed=0

  if [[ ${#TESTS_TO_RUN[@]} -gt 0 ]]; then
    if (( RUN_BUILD_TESTS || RUN_UNIT_TESTS || RUN_INTEGRATION_TESTS )); then
      abort "--run-test must be used alone"
    fi
    # If this is a presubmit run, but a documentation-only PR, don't run the test
    if (( IS_PRESUBMIT && IS_DOCUMENTATION_PR )); then
      header "Documentation only PR, skipping running custom test"
      exit 0
    fi
    for test_to_run in "${TESTS_TO_RUN[@]}"; do
      ${test_to_run} || { failed=1; step_failed "${test_to_run}"; }
    done
  fi

  run_build_tests || { failed=1; step_failed "run_build_tests"; }
  # If PRESUBMIT_TEST_FAIL_FAST is set to true, don't run unit tests if build tests failed
  if (( ! PRESUBMIT_TEST_FAIL_FAST )) || (( ! failed )); then
    run_unit_tests || { failed=1; step_failed "run_unit_tests"; }
  fi
  # If PRESUBMIT_TEST_FAIL_FAST is set to true, don't run integration tests if build/unit tests failed
  if (( ! PRESUBMIT_TEST_FAIL_FAST )) || (( ! failed )); then
    run_integration_tests || { failed=1; step_failed "run_integration_tests"; }
  fi

  exit ${failed}
}
