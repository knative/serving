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

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/library.sh

CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

KNATIVE_CODEGEN_PKG=${KNATIVE_CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 ./vendor/github.com/knative/pkg 2>/dev/null || echo ../pkg)}

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  github.com/knative/serving/pkg/client github.com/knative/serving/pkg/apis \
  "serving:v1alpha1,v1beta1 autoscaling:v1alpha1 networking:v1alpha1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Knative Injection
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  github.com/knative/serving/pkg/client github.com/knative/serving/pkg/apis \
  "serving:v1alpha1,v1beta1 autoscaling:v1alpha1 networking:v1alpha1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Generate our own client for cert-manager (otherwise injection won't work)
${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  github.com/knative/serving/pkg/client/certmanager github.com/jetstack/cert-manager/pkg/apis \
  "certmanager:v1alpha1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Knative Injection (for cert-manager)
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  github.com/knative/serving/pkg/client/certmanager github.com/jetstack/cert-manager/pkg/apis \
  "certmanager:v1alpha1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

# Depends on generate-groups.sh to install bin/deepcopy-gen
${GOPATH}/bin/deepcopy-gen \
  -O zz_generated.deepcopy \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt \
  -i github.com/knative/serving/pkg/apis/config \
  -i github.com/knative/serving/pkg/reconciler/clusteringress/config \
  -i github.com/knative/serving/pkg/reconciler/certificate/config \
  -i github.com/knative/serving/pkg/reconciler/configuration/config \
  -i github.com/knative/serving/pkg/reconciler/revision/config \
  -i github.com/knative/serving/pkg/reconciler/route/config \
  -i github.com/knative/serving/pkg/tracing/config \
  -i github.com/knative/serving/pkg/activator/config \
  -i github.com/knative/serving/pkg/autoscaler \
  -i github.com/knative/serving/pkg/deployment \
  -i github.com/knative/serving/pkg/gc \
  -i github.com/knative/serving/pkg/logging \
  -i github.com/knative/serving/pkg/metrics \
  -i github.com/knative/serving/pkg/network

# Make sure our dependencies are up-to-date
${REPO_ROOT_DIR}/hack/update-deps.sh
