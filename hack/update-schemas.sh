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

source $(dirname $0)/../vendor/knative.dev/hack/codegen-library.sh

group "Generate CRD schemas"
# Make sure you install the patched version of controller-gen from
# https://github.com/markusthoemmes/controller-tools/tree/knative-specific
#
# We need a patched version because
# 1. There's a bug that makes our URL types unusable
#    see https://github.com/kubernetes-sigs/controller-tools/issues/560
# 2. There's a missing feature to properly generate nested ObjectMeta
#    see https://github.com/kubernetes-sigs/controller-tools/pull/563
# 3. We need specialized logic to filter down the surface of PodSpec we allow in Knative.
controller-gen schemapatch:manifests=config/core/300-resources \
  output:dir=config/core/300-resources \
  paths=./pkg/apis/...
