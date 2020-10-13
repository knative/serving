#!/bin/bash

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

#!/usr/bin/env bash

# Download and unpack cert-manager
CERT_MANAGER_VERSION=1.0.0
YAML_URL=https://github.com/jetstack/cert-manager/releases/download/v${CERT_MANAGER_VERSION}/cert-manager.yaml

# Download the cert-manager yaml file
wget $YAML_URL

# Add enable-certificate-owner-ref option to cert-manager's controller.
# The option is to cleans up secret(certificate) by adding ownerref.
patch -l cert-manager.yaml owner-ref.patch
