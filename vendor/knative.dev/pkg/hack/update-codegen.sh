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

source $(dirname $0)/../vendor/knative.dev/hack/codegen-library.sh

# If we run with -mod=vendor here, then generate-groups.sh looks for vendor files in the wrong place.
export GOFLAGS=-mod=

echo "=== Update Codegen for $MODULE_NAME"

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.

group "Kubernetes Codegen"

# Knative Injection
${REPO_ROOT_DIR}/hack/generate-knative.sh "injection" \
  knative.dev/pkg/client knative.dev/pkg/apis \
  "duck:v1alpha1,v1beta1,v1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Based on: https://github.com/kubernetes/kubernetes/blob/8ddabd0da5cc54761f3216c08e99fa1a9f7ee2c5/hack/lib/init.sh#L116
# The '-path' is a hack to workaround the lack of portable `-depth 2`.
K8S_TYPES=$(find ./vendor/k8s.io/api -type d -path '*/*/*/*/*/*' | cut -d'/' -f 5-6 | sort | sed 's@/@:@g' |
  grep -v "admission:" | grep -v "imagepolicy:" | grep -v "abac:" | grep -v "componentconfig:")

OUTPUT_PKG="knative.dev/pkg/client/injection/kube" \
VERSIONED_CLIENTSET_PKG="k8s.io/client-go/kubernetes" \
EXTERNAL_INFORMER_PKG="k8s.io/client-go/informers" \
  ${REPO_ROOT_DIR}/hack/generate-knative.sh "injection" \
    k8s.io/client-go \
    k8s.io/api \
    "${K8S_TYPES}" \
    --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt \
    --force-genreconciler-kinds "Namespace,ConfigMap,Deployment,Secret,Pod,CronJob,NetworkPolicy,Node,ValidatingWebhookConfiguration,MutatingWebhookConfiguration"

OUTPUT_PKG="knative.dev/pkg/client/injection/apiextensions" \
VERSIONED_CLIENTSET_PKG="k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset" \
  ${REPO_ROOT_DIR}/hack/generate-knative.sh "injection" \
    k8s.io/apiextensions-apiserver/pkg/client \
    k8s.io/apiextensions-apiserver/pkg/apis \
    "apiextensions:v1beta1,v1" \
    --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt \
    --force-genreconciler-kinds "CustomResourceDefinition"

group "Knative Codegen"

# Only deepcopy the Duck types, as they are not real resources.
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

group "Update deps post-codegen"

# Make sure our dependencies are up-to-date
${REPO_ROOT_DIR}/hack/update-deps.sh
