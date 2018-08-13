#!/bin/bash

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

SERVING_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SERVING_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  github.com/knative/serving/vendor/github.com/knative/pkg/client github.com/knative/serving/vendor/github.com/knative/pkg/apis \
  "istio:v1alpha3"\
  --go-header-file ${SERVING_ROOT}/hack/boilerplate/boilerplate.go.txt

# Currently we are running code-generator for 'vendor' directory, so we need to replace package path
# as well as the interface names to keep consistent with our current way of using Istio APIs and clients.
[[ x$(uname) == "xDarwin" ]] && sedi=(-i '') || sedi=(-i)       
find vendor/github.com/knative/pkg/. -name '*.go' -exec grep -l atewaies {} \; | xargs sed "${sedi[@]}" 's/atewaies/ateways/g'
find vendor/github.com/knative/pkg/. -name '*.go' -exec grep -l  "github.com/knative/serving/vendor/github.com/knative/pkg/client" {} \; | xargs sed "${sedi[@]}" 's/github.com\/knative\/serving\/vendor\/github.com\/knative\/pkg/github.com\/knative\/pkg/g'
find vendor/github.com/knative/pkg/. -name '*.go' -exec grep -l IstioV1alpha3 {} \; | xargs sed "${sedi[@]}" 's/IstioV1alpha3/NetworkingV1alpha3/g'
find vendor/github.com/knative/pkg/. -name '*.go' -exec grep -l "Istio()" {} \; | xargs sed "${sedi[@]}" 's/Istio()/Networking()/g'
find vendor/github.com/knative/pkg/. -name '*.go' -exec grep -l istiov1alpha3 {} \; | xargs sed "${sedi[@]}" 's/istiov1alpha3/networkingv1alpha3/g'
