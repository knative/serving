kind delete cluster --name=knative
kind create cluster --name=knative

curl https://raw.githubusercontent.com/metallb/metallb/v0.13.5/config/manifests/metallb-native.yaml -k | \
sed '0,/args:/s//args:\n        - --webhook-mode=disabled/' | \
sed '/apiVersion: admissionregistration/,$d' | \
kubectl apply -f -
# Add Layer 2 config
network=$(docker network inspect kind -f "{{(index .IPAM.Config 0).Subnet}}" | cut -d '.' -f1,2)
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: first-pool
  namespace: metallb-system
spec:
  addresses:
  - $network.255.1-$network.255.250
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: example
  namespace: metallb-system
EOF

# kubectl apply -f ./third_party/cert-manager-latest/cert-manager.yaml
kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Established --all crd
kubectl wait --for=condition=Available -n cert-manager --all deployments

ko apply --selector knative.dev/crd-install=true -Rf config/core/
kubectl wait --for=condition=Established --all crd

ko apply -Rf config/core/

ko delete -f config/post-install/default-domain.yaml --ignore-not-found
ko apply -f config/post-install/default-domain.yaml

kubectl apply -f ./third_party/kourier-latest/kourier.yaml

kubectl patch configmap/config-network \
  -n knative-serving \
  --type merge \
  -p '{"data":{"ingress.class":"kourier.ingress.networking.knative.dev"}}'