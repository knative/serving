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

set -o errexit
set -o nounset
set -o pipefail

source $(dirname "$0")/../vendor/knative.dev/hack/library.sh

go_update_deps "$@"

# Match the SDK's resource schema, not the newest available semconv package.
log.step "Syncing semconv imports with the OTel SDK"
SEMCONV_IMPORT=$(go list -mod=mod -f '{{join .Imports "\n"}}' \
  go.opentelemetry.io/otel/sdk/resource | grep -F 'go.opentelemetry.io/otel/semconv/')

OLD_IMPORT=$(go list -mod=mod -f '{{join .Imports "\n"}}' \
  ./observability/semconv | grep -F 'go.opentelemetry.io/otel/semconv/')

if [ "$OLD_IMPORT" != "$SEMCONV_IMPORT" ]; then
  imports=$(go list -mod=mod -f '{{join .Imports "\n"}}' \
    ./observability/... | grep -F "$OLD_IMPORT" | sort -u)

  while IFS= read -r old_import; do
    new_import="${old_import/"$OLD_IMPORT"/"$SEMCONV_IMPORT"}"
    gofmt -w -r "\"${old_import}\" -> \"${new_import}\"" observability
  done <<< "$imports"

  # Refresh dependencies and vendoring after changing imports.
  go_update_deps
fi
