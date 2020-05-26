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

export GO111MODULE=on

source $(dirname $0)/../vendor/knative.dev/test-infra/scripts/library.sh

CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 $(dirname $0)/../vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.

# Knative Injection
${REPO_ROOT_DIR}/hack/generate-knative.sh "injection" \
  knative.dev/pkg/client knative.dev/pkg/apis \
  "duck:v1alpha1,v1beta1,v1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

OUTPUT_PKG="knative.dev/pkg/client/injection/kube" \
VERSIONED_CLIENTSET_PKG="k8s.io/client-go/kubernetes" \
EXTERNAL_INFORMER_PKG="k8s.io/client-go/informers" \
  ${REPO_ROOT_DIR}/hack/generate-knative.sh "injection" \
    k8s.io/client-go \
    k8s.io/api \
    "admissionregistration:v1beta1 apps:v1 autoscaling:v1,v2beta1 batch:v1,v1beta1 core:v1 rbac:v1" \
    --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt \
    --force-genreconciler-kinds "Namespace"

OUTPUT_PKG="knative.dev/pkg/client/injection/apiextensions" \
VERSIONED_CLIENTSET_PKG="k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset" \
  ${REPO_ROOT_DIR}/hack/generate-knative.sh "injection" \
    k8s.io/apiextensions-apiserver/pkg/client \
    k8s.io/apiextensions-apiserver/pkg/apis \
    "apiextensions:v1beta1" \
    --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt \
    --force-genreconciler-kinds "CustomResourceDefinition"

# Only deepcopy the Duck types, as they are not real resources.
chmod +x ${CODEGEN_PKG}/generate-groups.sh
${CODEGEN_PKG}/generate-groups.sh "deepcopy" \
  knative.dev/pkg/client knative.dev/pkg/apis \
  "duck:v1alpha1,v1beta1,v1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Depends on generate-groups.sh to install bin/deepcopy-gen
go run k8s.io/code-generator/cmd/deepcopy-gen  --input-dirs \
  $(echo \
  knative.dev/pkg/apis \
  knative.dev/pkg/tracker \
  knative.dev/pkg/logging \
  knative.dev/pkg/metrics \
  knative.dev/pkg/testing \
  knative.dev/pkg/testing/duck \
  knative.dev/pkg/webhook/resourcesemantics/conversion/internal \
  | sed "s/ /,/g") \
  -O zz_generated.deepcopy \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Make sure our dependencies are up-to-date
${REPO_ROOT_DIR}/hack/update-deps.sh

