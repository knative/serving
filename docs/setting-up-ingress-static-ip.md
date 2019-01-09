# Setting Up Static IP for Knative Gateway

Knative uses a shared Gateway to serve all incoming traffic within Knative
service mesh, which is the "knative-shared-gateway" Gateway under
"knative-serving" namespace. The IP address to access the gateway is the
external IP address of the "istio-ingressgateway" service under the
"istio-system" namespace. So in order to set static IP for the Knative shared
gateway, you just need to set the external IP address of the
"istio-ingressgateway" service to the static IP you need. If the gateway service
has been replaced to that of other service, you'll need to replace
"istio-ingressgateway" with the service name accordingly. See
[instructions](setting-up-custom-ingress-gateway.md) for more details.

## Prerequisites

### Prerequisite 1: Reserve a static IP

#### Knative on GKE

If you are running Knative cluster on GKE, you can follow the
[instructions](https://cloud.google.com/compute/docs/ip-addresses/reserve-static-external-ip-address#reserve_new_static)
to reserve a REGIONAL IP address. The region of the IP address should be the
region your Knative cluster is running in (e.g. us-east1, us-central1, etc.).

TODO: add documentation on reserving static IP in other cloud platforms.

### Prerequisite 2: Deploy Istio And Knative Serving

Follow the [instructions](../DEVELOPMENT.md) to deploy Istio and Knative Serving
into your cluster.

Once you reach this point, you can start to set up static IP for Knative
gateway.

## Set Up Static IP for Knative Gateway

### Step 1: Update external IP of "istio-ingressgateway" service

Run following command to reset the external IP for the "istio-ingressgateway"
service to the static IP you reserved.

```shell
kubectl patch svc istio-ingressgateway -n istio-system --patch '{"spec": { "loadBalancerIP": "<your-reserved-static-ip>" }}'
```

### Step 2: Verify static IP address of istio-ingressgateway service

You can check the external IP of the "istio-ingressgateway" service with:

```shell
kubectl get svc istio-ingressgateway -n istio-system
```

The result should be something like

```console
NAME                     TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                                         AGE
istio-ingressgateway     LoadBalancer   10.50.250.120   35.210.48.100   80:31380/TCP,443:31390/TCP,31400:31400/TCP...   5h
```

The external IP will be eventually set to the static IP. This process could take
several minutes.
