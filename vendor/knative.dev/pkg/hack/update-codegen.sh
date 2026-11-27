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

cd "$(dirname "${BASH_SOURCE[0]}")/.."
MODULE_NAME="$(go list -m)"
boilerplate="hack/boilerplate/boilerplate.go.txt"

function injection() {
  local output_dir=$1 client_pkg=$2
  shift 2
  go run knative.dev/pkg/codegen/cmd/injection-gen \
    --versioned-clientset-package "${client_pkg}/clientset/versioned" \
    --external-versions-informers-package "${client_pkg}/informers/externalversions" \
    --listers-package "${client_pkg}/listers" \
    --output-package "${MODULE_NAME}/${output_dir}" \
    --output-dir "${output_dir}" \
    --go-header-file "${boilerplate}" \
    "$@"
}

echo "=== Update Codegen for ${MODULE_NAME}"
echo "=== Knative Codegen"

# Discover Kubernetes API packages, excluding unsupported groups and alpha APIs.
K8S_TYPES=$(go list k8s.io/api/... |
  grep -Ev '/(abac|admission|componentconfig|imagepolicy|resource|storagemigration)/|alpha' |
  LC_ALL=C sort | paste -sd , -)

# Knative Injection
injection client/injection knative.dev/pkg/client \
  --input-dirs=knative.dev/pkg/apis/duck/{v1alpha1,v1beta1,v1}

# Kubernetes Injection
kube_reconciler_kinds=(
  Namespace
  ConfigMap
  Deployment
  Secret
  Service
  Pod
  CronJob
  NetworkPolicy
  Node
  ValidatingWebhookConfiguration
  MutatingWebhookConfiguration
  ServiceAccount
)
injection client/injection/kube k8s.io/client-go \
  --input-dirs "${K8S_TYPES}" \
  --versioned-clientset-package k8s.io/client-go/kubernetes \
  --external-versions-informers-package k8s.io/client-go/informers \
  --force-genreconciler-kinds "$(IFS=,; echo "${kube_reconciler_kinds[*]}")"

# API Extensions Injection
injection client/injection/apiextensions k8s.io/apiextensions-apiserver/pkg/client \
  --input-dirs=k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/{v1beta1,v1} \
  --versioned-clientset-package k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset \
  --force-genreconciler-kinds "CustomResourceDefinition"

echo "=== Kubernetes Codegen"

# Only generate deepcopy helpers; Duck types are not real resources.
go run k8s.io/code-generator/cmd/deepcopy-gen \
  --output-file zz_generated.deepcopy.go \
  --go-header-file "${boilerplate}" \
  ./...

echo "=== Update deps post-codegen"

# Make sure our dependencies are up-to-date.
./hack/update-deps.sh
