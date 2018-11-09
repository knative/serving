# Download and unpack Istio
ISTIO_VERSION=1.1.0-snapshot.2
BASE_URL=https://gcsweb.istio.io/gcs/istio-prerelease/prerelease/
DOWNLOAD_URL=${BASE_URL}/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-linux.tar.gz

wget $DOWNLOAD_URL
tar xzf istio-${ISTIO_VERSION}-linux.tar.gz
cd istio-${ISTIO_VERSION}

# Copy CRDs template
cp install/kubernetes/helm/istio/templates/crds.yaml ../istio-crds.yaml

# Create template
helm init --client-only

helm dep update install/kubernetes/helm/istio

helm template install/kubernetes/helm/istio \
  --name=istio --namespace=istio-system \
  --set sidecarInjectorWebhook.enabled=true \
  --set sidecarInjectorWebhook.enableNamespacesByDefault=true \
  --set global.proxy.autoInject=disabled \
  --set prometheus.enabled=false \
  > ../istio.yaml

helm template --name=istio --namespace=istio-system \
  --set sidecarInjectorWebhook.enabled=false \
  --set global.proxy.autoInject=disabled \
  --set global.omitSidecarInjectorConfigMap=true \
  --set prometheus.enabled=false \
  install/kubernetes/helm/istio > ../istio-lean.yaml

# Clean up.
cd ..
rm -rf istio-${ISTIO_VERSION}
rm istio-${ISTIO_VERSION}-linux.tar.gz

# Add in the `istio-system` namespace, so we only need to
# run one kubectl command to install istio.
patch istio.yaml namespace.yaml.patch
patch istio-lean.yaml namespace.yaml.patch
