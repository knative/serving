#!/usr/bin/env bash

# Copyright 2019 The Knative Authors
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

function microbenchmarks_run() {
  if [ "$1" != "" ]; then
    OUTPUT_FILE="$1"
  else
    OUTPUT_FILE="${ARTIFACTS:-$(mktemp -d)}/bench-result.txt"
  fi

  echo "Output will be at $OUTPUT_FILE"

  # Run all microbenchmarks
  go clean
  go test -bench=. -benchmem -run="^$" -v ./...   >> "$OUTPUT_FILE" || exit
}

function microbenchmarks_run_and_compare() {
  if [ "$1" == "" ] || [ $# -gt 1 ]; then
    echo "Error: Expecting an argument" >&2
    echo "usage: $(basename $0) revision_to_compare" >&2
    exit 1
  fi

  # Benchstat is required to compare the bench results
  GO111MODULE=off go get golang.org/x/perf/cmd/benchstat

  # Revision to use to compare with actual
  REVISION="$1"
  OUTPUT_DIR=${ARTIFACTS:-$(mktemp -d)}

  echo "Outputs will be at $OUTPUT_DIR"

  # Run this revision benchmarks
  microbenchmarks_run "$OUTPUT_DIR/new.txt"

  # Run other revision benchmarks
  git checkout "$REVISION"
  microbenchmarks_run "$OUTPUT_DIR/old.txt"

  # Print results in console
  benchstat "$OUTPUT_DIR/old.txt" "$OUTPUT_DIR/new.txt"

  # Generate html results
  benchstat -html "$OUTPUT_DIR/old.txt" "$OUTPUT_DIR/new.txt" >> "$OUTPUT_DIR/results.html"
}
