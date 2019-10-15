#!/usr/bin/env bash

# Download and unpack Istio
ISTIO_VERSION=1.3.3
DOWNLOAD_URL=https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-linux.tar.gz

wget --no-check-certificate $DOWNLOAD_URL
if [ $? != 0 ]; then
  echo "Failed to download istio package"
  exit 1
fi
tar xzf istio-${ISTIO_VERSION}-linux.tar.gz

( # subshell in downloaded directory
cd istio-${ISTIO_VERSION} || exit

# Create CRDs template
helm template --namespace=istio-system \
  install/kubernetes/helm/istio-init \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > ../istio-crds.yaml

# Create a custom cluster local gateway, based on the Istio custom-gateway template.
helm template --namespace=istio-system install/kubernetes/helm/istio --values ../values-extras.yaml \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > ../istio-knative-extras.yaml

# A template with sidecar injection enabled.
helm template --namespace=istio-system install/kubernetes/helm/istio --values ../values.yaml \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > ../istio.yaml

# A lighter template, with just pilot/gateway.
# Based on install/kubernetes/helm/istio/values-istio-minimal.yaml
helm template --namespace=istio-system install/kubernetes/helm/istio --values ../values-lean.yaml \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > ../istio-lean.yaml
)

# Clean up.
rm -rf istio-${ISTIO_VERSION}
rm istio-${ISTIO_VERSION}-linux.tar.gz

# Add in the `istio-system` namespace to reduce number of commands.
patch istio-crds.yaml namespace.yaml.patch
patch istio.yaml namespace.yaml.patch
patch istio-lean.yaml namespace.yaml.patch

# Increase termination drain duration seconds.
patch -l istio.yaml drain-seconds.yaml.patch
