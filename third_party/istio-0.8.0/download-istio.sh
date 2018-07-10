# Download and unpack Istio
wget https://github.com/istio/istio/releases/download/0.8.0/istio-0.8.0-linux.tar.gz
tar xzf istio-0.8.0-linux.tar.gz
cd istio-0.8.0

# Create template
helm template --namespace=istio-system \
    --set sidecarInjectorWebhook.enabled=true \
    --set global.proxy.image=proxyv2 \
    --set prometheus.enabled=false \
    install/kubernetes/helm/istio > ../istio.yaml

# Clean up.
cd ..
rm -rf istio-0.8.0
rm istio-0.8.0-linux.tar.gz
