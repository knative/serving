#!/usr/bin/env bash

# Download and unpack cert-manager
CERT_MANAGER_VERSION=0.6.1
DOWNLOAD_URL=https://github.com/jetstack/cert-manager/archive/v${CERT_MANAGER_VERSION}.tar.gz

wget $DOWNLOAD_URL
tar xzf v${CERT_MANAGER_VERSION}.tar.gz

( # subshell in downloaded directory
cd cert-manager-${CERT_MANAGER_VERSION} || exit

# Copy the CRD yaml file
cp deploy/manifests/00-crds.yaml ../cert-manager-crds.yaml

# Copy the cert-manager yaml file
cp deploy/manifests/cert-manager.yaml ../cert-manager.yaml
)

# Clean up.
rm -rf cert-manager-${CERT_MANAGER_VERSION}
rm v${CERT_MANAGER_VERSION}.tar.gz

