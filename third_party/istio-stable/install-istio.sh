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

# Download and unpack Istio
ISTIO_VERSION=1.6.9
ISTIO_TARBALL=istio-${ISTIO_VERSION}-linux-amd64.tar.gz
DOWNLOAD_URL=https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/${ISTIO_TARBALL}

wget --no-check-certificate $DOWNLOAD_URL
if [ $? != 0 ]; then
  echo "Failed to download Istio package"
  exit 1
fi
tar xzf ${ISTIO_TARBALL} 

# Install Istio
./istio-${ISTIO_VERSION}/bin/istioctl manifest apply -f "$(dirname $0)/$1"

# For 1.7+
#./istio-${ISTIO_VERSION}/bin/istioctl manifest install -f "$(dirname $0)/$1"

# Clean up
rm -rf istio-${ISTIO_VERSION}
rm ${ISTIO_TARBALL}

## Add in the `istio-system` namespace to reduce number of commands.
#patch istio-crds.yaml namespace.yaml.patch
#patch istio-ci-mesh.yaml namespace.yaml.patch
#patch istio-ci-no-mesh.yaml namespace.yaml.patch
#patch istio-minimal.yaml namespace.yaml.patch
#
## Increase termination drain duration seconds.
#patch -l istio-ci-mesh.yaml drain-seconds.yaml.patch

