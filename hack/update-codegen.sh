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

boilerplate="${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt"

echo "=== Update Codegen for $MODULE_NAME"

# Parse flags to determine if we should generate protobufs.
generate_protobufs=0
while [[ $# -ne 0 ]]; do
  parameter=$1
  case ${parameter} in
    --generate-protobufs) generate_protobufs=1 ;;
    *) abort "unknown option ${parameter}" ;;
  esac
  shift
done
readonly generate_protobufs

if (( generate_protobufs )); then
  group "Generating protocol buffer code"
  protos=$(find "${REPO_ROOT_DIR}/pkg" "${REPO_ROOT_DIR}/test" -name '*.proto')
  for proto in $protos
  do
    protoc "$proto" -I="${REPO_ROOT_DIR}" --gogofaster_out=plugins=grpc:.

    # Add license headers to the generated files too.
    dir=$(dirname "$proto")
    base=$(basename "$proto" .proto)
    generated="${dir}/${base}.pb.go"
    echo -e "$(cat "${boilerplate}")\n\n$(cat "${generated}")" > "${generated}"
  done
fi

group "Generating checksums for configmap _example keys"

${REPO_ROOT_DIR}/hack/update-checksums.sh

group "Kubernetes Codegen"

source "${CODEGEN_PKG}/kube_codegen.sh"

kube::codegen::gen_client \
  --boilerplate "${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt" \
  --output-dir "${REPO_ROOT_DIR}/pkg/client" \
  --output-pkg "knative.dev/serving/pkg/client" \
  --with-watch \
  "${REPO_ROOT_DIR}/pkg/apis"

group "Knative Codegen"

# Knative Injection
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  knative.dev/serving/pkg/client knative.dev/serving/pkg/apis \
  "serving:v1 serving:v1beta1 autoscaling:v1alpha1" \
  --go-header-file "${boilerplate}"

# Knative Injection (for cert-manager)
OUTPUT_PKG="knative.dev/serving/pkg/client/certmanager/injection" \
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  github.com/cert-manager/cert-manager/pkg/client github.com/cert-manager/cert-manager/pkg/apis "certmanager:v1 acme:v1" \
  --disable-informer-init \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

group "Deepcopy Gen"

kube::codegen::gen_helpers \
  --boilerplate "${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt" \
  "${REPO_ROOT_DIR}/pkg"

group "Generating API reference docs"

${REPO_ROOT_DIR}/hack/update-reference-docs.sh

group "Generating schemas"

${REPO_ROOT_DIR}/hack/update-schemas.sh

group "Update deps post-codegen"

# Make sure our dependencies are up-to-date
${REPO_ROOT_DIR}/hack/update-deps.sh
