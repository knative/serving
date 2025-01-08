#!/usr/bin/env bash

# Copyright 2021 The Knative Authors
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



# Create a backup for every linked CRD.
links=$(find "$(dirname "$0")/../config/core/300-resources" -type l)
for link in $links; do
  cp "$link" "$link.bkp"
done

go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5 \
  schemapatch:manifests=config/core/300-resources,generateEmbeddedObjectMeta=true \
  output:dir=config/core/300-resources \
  paths=./pkg/apis/...

go run ./cmd/schema-tweak

# Restore linked CRDs.
for link in $links; do
  cat "$link.bkp" > "$link"
  rm "$link.bkp"
done
