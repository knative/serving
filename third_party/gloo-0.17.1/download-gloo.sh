#!/usr/bin/env bash

set -ex

# Download and unpack Gloo
GLOO_VERSION=0.17.1
GLOO_CHART=gloo-${GLOO_VERSION}.tgz
DOWNLOAD_URL=https://storage.googleapis.com/solo-public-helm/charts/${GLOO_CHART}

wget $DOWNLOAD_URL

# Create CRDs template
helm template --namespace=gloo-system \
  ${GLOO_CHART} --values values.yaml \
  `# Removing trailing whitespaces to make automation happy` \
  | sed 's/[ \t]*$//' \
  > gloo.yaml

# Clean up.
rm ${GLOO_CHART}
