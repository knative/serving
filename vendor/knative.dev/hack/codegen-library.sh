#!/usr/bin/env bash

# Copyright 2020 The Knative Authors
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

# Setup the env for doing Knative style codegen.

kn_hack_library=${kn_hack_library:-"$(dirname $0)/../vendor/knative.dev/hack/library.sh"}

if [[ -f "$kn_hack_library" ]]; then
  source $kn_hack_library
else
  echo "this file is intended to be imported from a golang project that vendors knative.dev/hack"
  exit
fi

export MODULE_NAME=$(go_mod_module_name)
export GOPATH=$(go_mod_gopath_hack)
export GOBIN=${GOPATH}/bin # Set GOBIN explicitly as deepcopy-gen is installed by go install.
export CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}
export KNATIVE_CODEGEN_PKG=${KNATIVE_CODEGEN_PKG:-$(cd ${REPO_ROOT_DIR}; ls -d -1 ./vendor/knative.dev/pkg 2>/dev/null || echo "${REPO_ROOT_DIR}")}

chmod +x ${CODEGEN_PKG}/generate-groups.sh
chmod +x ${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh
