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

# Download Kong
KONG_VERSION=0.9.x
KONG_YAML=all-in-one-dbless.yaml
DOWNLOAD_URL=https://raw.githubusercontent.com/Kong/kubernetes-ingress-controller/${KONG_VERSION}/deploy/single/${KONG_YAML}

wget -O kong.yaml ${DOWNLOAD_URL}
