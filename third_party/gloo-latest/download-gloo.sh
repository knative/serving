#!/usr/bin/env bash

set -ex

# Download and unpack Gloo
GLOO_VERSION=0.18.12
GLOO_CHART=gloo-${GLOO_VERSION}.tgz
DOWNLOAD_URL=https://storage.googleapis.com/solo-public-helm/charts/${GLOO_CHART}

wget ${DOWNLOAD_URL}
tar xvf ${GLOO_CHART}

# Create CRDs template
helm template --namespace=gloo-system \
  ${GLOO_CHART} --values gloo/values-knative.yaml --values value-overrides.yaml \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > gloo.yaml

# Clean up.
rm ${GLOO_CHART}
rm -rf gloo/
