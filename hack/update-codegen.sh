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
# If we run with -mod=vendor here, then generate-groups.sh looks for vendor files in the wrong place.
export GOFLAGS=-mod=

if [ -z "${GOPATH:-}" ]; then
  export GOPATH=$(go env GOPATH)
fi

source $(dirname $0)/../vendor/knative.dev/test-infra/scripts/library.sh

boilerplate="${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt"

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
  echo "Generating protocol buffer code"
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

echo "Generating checksums for configmap _example keys"
${REPO_ROOT_DIR}/hack/update-checksums.sh

CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 $(dirname $0)/../vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}
KNATIVE_CODEGEN_PKG=${KNATIVE_CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 $(dirname $0)/../vendor/knative.dev/pkg 2>/dev/null || echo ../pkg)}

chmod +x ${CODEGEN_PKG}/generate-groups.sh
chmod +x ${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  knative.dev/serving/pkg/client knative.dev/serving/pkg/apis \
  "serving:v1alpha1,v1beta1,v1 autoscaling:v1alpha1" \
  --go-header-file "${boilerplate}"

# Knative Injection
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  knative.dev/serving/pkg/client knative.dev/serving/pkg/apis \
  "serving:v1alpha1,v1beta1,v1 autoscaling:v1alpha1" \
  --go-header-file "${boilerplate}"

# Depends on generate-groups.sh to install bin/deepcopy-gen
${GOPATH}/bin/deepcopy-gen \
  -O zz_generated.deepcopy \
  --go-header-file "${boilerplate}" \
  -i knative.dev/serving/pkg/apis/config \
  -i knative.dev/serving/pkg/reconciler/gc/config \
  -i knative.dev/serving/pkg/reconciler/revision/config \
  -i knative.dev/serving/pkg/reconciler/route/config \
  -i knative.dev/serving/pkg/activator/config \
  -i knative.dev/serving/pkg/autoscaler \
  -i knative.dev/serving/pkg/autoscaler/config \
  -i knative.dev/serving/pkg/autoscaler/config/autoscalerconfig \
  -i knative.dev/serving/pkg/deployment \
  -i knative.dev/serving/pkg/gc \
  -i knative.dev/serving/pkg/logging \
  -i knative.dev/serving/pkg/metrics

# Make sure our dependencies are up-to-date
${REPO_ROOT_DIR}/hack/update-deps.sh
