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

# Generate manifest files from IstioOperator.
istioctl manifest generate -f istio-ci-mesh-operator.yaml > istio-ci-mesh.yaml
istioctl manifest generate -f istio-ci-no-mesh-operator.yaml > istio-ci-no-mesh.yaml
istioctl manifest generate -f istio-minimal-operator.yaml > istio-minimal.yaml

# Generate istio-crds.yaml
output=$(mktemp -d)
istioctl manifest generate -f istio-ci-mesh-operator.yaml -o ${output}

# Istioctl manifest generate does not generate namespace from 1.6 - https://github.com/istio/istio/pull/22549
cat <<EOF > istio-crds.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: istio-system
  labels:
    istio-operator-managed: Reconcile
    istio-injection: disabled
---
EOF
cat ${output}/Base/Base.yaml >> istio-crds.yaml
