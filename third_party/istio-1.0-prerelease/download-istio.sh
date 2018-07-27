# Download and unpack Istio
ISTIO_VERSION=release-1.0-20180723-09-15
DOWNLOAD_URL=https://storage.googleapis.com/istio-prerelease/daily-build/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-linux.tar.gz
wget $DOWNLOAD_URL
tar xzf istio-${ISTIO_VERSION}-linux.tar.gz
cd istio-${ISTIO_VERSION}

# Create template
helm template --namespace=istio-system \
  --set sidecarInjectorWebhook.enabled=true \
  --set sidecarInjectorWebhook.enableNamespacesByDefault=true \
  --set global.proxy.image=proxyv2 \
  --set global.proxy.autoInject=disabled \
  --set prometheus.enabled=false \
  install/kubernetes/helm/istio > ../istio.yaml

# Clean up.
cd ..
rm -rf istio-${ISTIO_VERSION}
rm istio-${ISTIO_VERSION}-linux.tar.gz

# Add in the `istio-system` namespace, so we only need to
# run one kubectl command to install istio.
patch istio.yaml namespace.yaml.patch
