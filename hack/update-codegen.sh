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

if [ -z "${GOPATH:-}" ]; then
  export GOPATH=$(go env GOPATH)
fi

source $(dirname $0)/../vendor/knative.dev/test-infra/scripts/library.sh

CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 $(dirname $0)/../vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

KNATIVE_CODEGEN_PKG=${KNATIVE_CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 $(dirname $0)/../vendor/knative.dev/pkg 2>/dev/null || echo ../pkg)}

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  knative.dev/serving/pkg/client knative.dev/serving/pkg/apis \
  "serving:v1alpha1,v1beta1,v1 autoscaling:v1alpha1 networking:v1alpha1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Knative Injection
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  knative.dev/serving/pkg/client knative.dev/serving/pkg/apis \
  "serving:v1alpha1,v1beta1,v1 autoscaling:v1alpha1 networking:v1alpha1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Generate our own client for istio (otherwise injection won't work)
${CODEGEN_PKG}/generate-groups.sh "client,informer,lister" \
  knative.dev/serving/pkg/client/istio istio.io/client-go/pkg/apis \
  "networking:v1alpha3" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Knative Injection (for istio)
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  knative.dev/serving/pkg/client/istio istio.io/client-go/pkg/apis \
  "networking:v1alpha3" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Generate our own client for cert-manager (otherwise injection won't work)
${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  knative.dev/serving/pkg/client/certmanager github.com/jetstack/cert-manager/pkg/apis \
  "certmanager:v1alpha2 acme:v1alpha2" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Knative Injection (for cert-manager)
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  knative.dev/serving/pkg/client/certmanager github.com/jetstack/cert-manager/pkg/apis \
  "certmanager:v1alpha2 acme:v1alpha2" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Depends on generate-groups.sh to install bin/deepcopy-gen
${GOPATH}/bin/deepcopy-gen \
  -O zz_generated.deepcopy \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt \
  -i knative.dev/serving/pkg/apis/config \
  -i knative.dev/serving/pkg/reconciler/ingress/config \
  -i knative.dev/serving/pkg/reconciler/certificate/config \
  -i knative.dev/serving/pkg/reconciler/gc/config \
  -i knative.dev/serving/pkg/reconciler/revision/config \
  -i knative.dev/serving/pkg/reconciler/route/config \
  -i knative.dev/serving/pkg/activator/config \
  -i knative.dev/serving/pkg/autoscaler \
  -i knative.dev/serving/pkg/deployment \
  -i knative.dev/serving/pkg/gc \
  -i knative.dev/serving/pkg/logging \
  -i knative.dev/serving/pkg/metrics \
  -i knative.dev/serving/pkg/network

# Make sure our dependencies are up-to-date
${REPO_ROOT_DIR}/hack/update-deps.sh
