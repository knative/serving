#!/usr/bin/env bash

# Copyright 2019 The Knative Authors
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

set -ex

# Download Kourier
KOURIER_VERSION=0.3.4
KOURIER_YAML=kourier-knative.yaml
DOWNLOAD_URL=https://raw.githubusercontent.com/3scale/kourier/v${KOURIER_VERSION}/deploy/${KOURIER_YAML}

wget ${DOWNLOAD_URL}

cat <<EOF > kourier.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kourier-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-logging
  namespace: kourier-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-observability
  namespace: kourier-system
---
EOF

cat ${KOURIER_YAML} \
  `# Install Kourier into the kourier-system namespace` \
  | sed 's/namespace: knative-serving/namespace: kourier-system/' \
  `# Expose Kourier services with LoadBalancer IPs instead of ClusterIP` \
  | sed 's/ClusterIP/LoadBalancer/' \
  >> kourier.yaml

# Clean up.
rm ${KOURIER_YAML}
