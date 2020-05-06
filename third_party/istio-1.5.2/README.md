The IstioOperator are:

- istio-ci-no-mesh.yaml: used in our continuous testing of Knative with Istio
  having sidecar disabled. This is also the setting that we use in our presubmit
  tests.
- istio-ci-mesh.yaml: used in our continuous testing of Knative with Istio
  having sidecar and mTLS enabled.
- istio-minimal.yaml: a minimal Istio installation used for development
  purposes.

To install istio by using these manifest, you can use `istioctl` (v1.5.1 or
later is required):

```
istioctl manifest apply -f istio-minimal-operator.yaml
```

or `kubectl`:

```
kubectl apply -f istio-crds.yaml
while [[ $(kubectl get crd gateways.networking.istio.io -o jsonpath='{.status.conditions[?(@.type=="Established")].status}') != 'True' ]]; do
  echo "Waiting on Istio CRDs"; sleep 1
done
kubectl apply -f istio-minimal.yaml
```
