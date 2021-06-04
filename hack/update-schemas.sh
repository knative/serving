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

# Install patched schemagen into a temporary directory.
#
# We need a patched version because
# 1. There's a bug that makes our URL types unusable
#    see https://github.com/kubernetes-sigs/controller-tools/issues/560
# 2. We need specialized logic to filter down the surface of PodSpec we allow in Knative.
#    The respective config for this is in `schemapatch-config.yaml`
export GOBIN
GOBIN=$(mktemp -d)
export PATH="$GOBIN:$PATH"

(
  cd "$GOBIN"
  mkdir controller-tools
  cd controller-tools
  go mod init tools
  # Pinned for reproducible builds.
  go mod edit -replace=sigs.k8s.io/controller-tools@v0.5.0=github.com/markusthoemmes/controller-tools@505dce98ec1d85fd566d13a6b55b8c19deeb765e 
  go get -d sigs.k8s.io/controller-tools/cmd/controller-gen@v0.5.0
  go install sigs.k8s.io/controller-tools/cmd/controller-gen
)

# Create a backup for every linked CRD.
links=$(find "$(dirname "$0")/../config/core/300-resources" -type l)
for link in $links; do
  cp "$link" "$link.bkp"
done

SCHEMAPATCH_CONFIG_FILE="$(dirname $0)/schemapatch-config.yaml" controller-gen \
  schemapatch:manifests=config/core/300-resources,generateEmbeddedObjectMeta=true \
  output:dir=config/core/300-resources \
  paths=./pkg/apis/...

# Restore linked CRDs.
for link in $links; do
  cat "$link.bkp" > "$link"
  rm "$link.bkp"
done
