#!/usr/bin/env bash

# Download and unpack Istio
ISTIO_VERSION=1.1.7
DOWNLOAD_URL=https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-linux.tar.gz

wget $DOWNLOAD_URL
tar xzf istio-${ISTIO_VERSION}-linux.tar.gz

( # subshell in downloaded directory
cd istio-${ISTIO_VERSION} || exit

# Create CRDs template
helm template --namespace=istio-system \
  install/kubernetes/helm/istio-init \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > ../istio-crds.yaml

# A template with sidecar injection enabled.
helm template --namespace=istio-system install/kubernetes/helm/istio --values ../values.yaml \
  `# Increase Istio control plane resources usage.` \
  --set gateways.istio-ingressgateway.resources.requests.cpu=500m \
  --set gateways.istio-ingressgateway.resources.requests.memory=256Mi \
  --set gateways.istio-ingressgateway.sds.enabled=true \
  --set pilot.autoscaleMin=2 \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > ../istio.yaml

# A lighter template, with just pilot/gateway.
# Based on install/kubernetes/helm/istio/values-istio-minimal.yaml
helm template --namespace=istio-system install/kubernetes/helm/istio --values ../values.yaml \
  `# Disable everything other than pilot and gateways` \
  --set mixer.enabled=false \
  --set mixer.policy.enabled=false \
  --set mixer.telemetry.enabled=false \
  --set pilot.sidecar=false \
  --set pilot.resources.requests.memory=128Mi \
  --set galley.enabled=false \
  --set global.useMCP=false \
  --set security.enabled=false \
  --set sidecarInjectorWebhook.enabled=false \
  --set global.omitSidecarInjectorConfigMap=true \
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

patch -l istio.yaml prestop-sleep.yaml.patch
