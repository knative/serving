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

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT_DIR=$(dirname $0)/..
cd ${REPO_ROOT_DIR}

if [[ -z "$(which golint)" ]]; then
  echo 'Can not find golint, install with:'
  echo 'go get -u github.com/golang/lint/golint'
  exit 1
fi

array_contains () {
    local seeking=$1; shift # shift will iterate through the array
    local in=1 # in holds the exit status for the function
    for element; do
        if [[ "$element" == "$seeking" ]]; then
            in=0 # set in to 0 since we found it
            break
        fi
    done
    return $in
}

# Check that the file is in alphabetical order
failure_file="${REPO_ROOT_DIR}/hack/.golint_failures"
if ! diff -u "${failure_file}" <(LC_ALL=C sort "${failure_file}"); then
  {
    echo
    echo "hack/.golint_failures is not in alphabetical order. Please sort it:"
    echo
    echo "  LC_ALL=C sort -o hack/.golint_failures hack/.golint_failures"
    echo
  } >&2
  false
fi

export IFS=$'\n'
# NOTE: when "go list -e ./..." is run within GOPATH, it turns the github.com/knative/serving
# as the prefix, however if we run it outside it returns the full path of the file
# with a leading underscore. We'll need to support both scenarios for all_packages.
all_packages=(
  $(go list -e ./... | egrep -v "/(third_party|vendor|client)" | sed -e 's|^github.com/knative/serving/||' -e "s|^_${REPO_ROOT_DIR}/\?||")
)
failing_packages=(
  $(cat $failure_file)
)
unset IFS
errors=()
not_failing=()
for p in "${all_packages[@]}"; do
  # Run golint on package/*.go file explicitly to validate all go files
  # and not just the ones for the current platform. This also will ensure that
  # _test.go files are linted.
  # Generated files are ignored, and each file is passed through golint
  # individually, as if one file in the package contains a fatal error (such as
  # a foo package with a corresponding foo_test package), golint seems to choke
  # completely.
  # Ref: https://github.com/golang/lint/issues/68
  failedLint=$(ls "$p"/*.go | egrep -v "(zz_generated.*.go|generated.pb.go|generated.proto|types_swagger_doc_generated.go)" | xargs -L1 golint 2>/dev/null)
  array_contains "$p" "${failing_packages[@]}" && in_failing=$? || in_failing=$?
  if [[ -n "${failedLint}" ]] && [[ "${in_failing}" -ne "0" ]]; then
    errors+=( "${failedLint}" )
  fi
  if [[ -z "${failedLint}" ]] && [[ "${in_failing}" -eq "0" ]]; then
    not_failing+=( $p )
  fi
done

# Check that all failing_packages actually still exist
gone=()
for p in "${failing_packages[@]}"; do
  array_contains "$p" "${all_packages[@]}" || gone+=( "$p" )
done

# Check to be sure all the packages that should pass lint are.
if [ ${#errors[@]} -eq 0 ]; then
  echo 'Congratulations!  All Go source files have been linted.'
else
  {
    echo "Errors from golint:"
    for err in "${errors[@]}"; do
      echo "$err"
    done
    echo
    echo 'Please review the above warnings. You can test via "golint" and commit the result.'
    echo 'If the above warnings do not make sense, you can exempt this package from golint'
    echo 'checking by adding it to hack/.golint_failures (if your reviewer is okay with it).'
    echo
  } >&2
  false
fi

if [[ ${#not_failing[@]} -gt 0 ]]; then
  {
    echo "Some packages in hack/.golint_failures are passing golint. Please remove them."
    echo
    for p in "${not_failing[@]}"; do
      echo "  $p"
    done
    echo
  } >&2
  false
fi

if [[ ${#gone[@]} -gt 0 ]]; then
  {
    echo "Some packages in hack/.golint_failures do not exist anymore. Please remove them."
    echo
    for p in "${gone[@]}"; do
      echo "  $p"
    done
    echo
  } >&2
  false
fi

