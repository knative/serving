# source setup_scripts/configure_kubectl.sh

curl https://raw.githubusercontent.com/metallb/metallb/v0.13.5/config/manifests/metallb-native.yaml -k | \
sed '0,/args:/s//args:\n        - --webhook-mode=disabled/' | \
sed '/apiVersion: admissionregistration/,$d' | \
kubectl apply -f -
# Add Layer 2 config
network=10.0.0
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: first-pool
  namespace: metallb-system
spec:
  addresses:
  - $network.10-$network.255
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: example
  namespace: metallb-system
EOF

# kubectl apply -f cert-manager.yaml
kubectl wait --for=condition=Established --all crd
kubectl wait --for=condition=Available -n cert-manager --all deployments

ko apply --selector knative.dev/crd-install=true -Rf config/core/ --platform linux/arm64
kubectl wait --for=condition=Established --all crd

ko apply -Rf config/core/ --platform linux/arm64

ko delete -f config/post-install/default-domain.yaml --ignore-not-found
ko apply -f config/post-install/default-domain.yaml --platform linux/arm64

kubectl apply -f ./third_party/kourier-latest/kourier.yaml

kubectl patch configmap/config-network \
  -n knative-serving \
  --type merge \
  -p '{"data":{"ingress.class":"kourier.ingress.networking.knative.dev"}}'

chdir setup_scripts
source elk_deploy.sh

read -p "Only press yes when elk services are running. (yes/no) " yn

case $yn in 
	yes ) echo ok, we will proceed;;
	no ) echo exiting...;
		exit;;
	* ) echo invalid response;
		exit 1;;
esac

# source configure_containerd.sh