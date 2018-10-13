# Setting Up Static IP for Knative Gateway

Knative uses a shared Gateway to serve all incoming traffic within Knative
service mesh, which is the "knative-shared-gateway" Gateway under
"knative-serving" namespace. The IP address to access the gateway is the
external IP address of the "knative-ingressgateway" service under the
"istio-system" namespace. So in order to set static IP for the Knative shared
gateway, you just need to set the external IP address of the
"knative-ingressgateway" service to the static IP you need.

## Prerequisites

### Prerequisite 1: Reserve a static IP

#### Knative on GKE

If you are running Knative cluster on GKE, you can follow the [instructions](https://cloud.google.com/compute/docs/ip-addresses/reserve-static-external-ip-address#reserve_new_static) to reserve a REGIONAL
IP address. The region of the IP address should be the region your Knative
 cluster is running in (e.g. us-east1, us-central1, etc.).

TODO: add documentation on reserving static IP in other cloud platforms.

### Prerequisite 2: Deploy Istio And Knative Serving

Follow the [instructions](https://github.com/knative/serving/blob/master/DEVELOPMENT.md)
to deploy Istio and Knative Serving into your cluster.

Once you reach this point, you can start to set up static IP for Knative
gateway.

## Set Up Static IP for Knative Gateway

### Step 1: Update external IP of "knative-ingressgateway" service

Run following command to reset the external IP for the
"knative-ingressgateway" service to the static IP you reserved.

```shell
kubectl patch svc knative-ingressgateway -n istio-system --patch '{"spec": { "loadBalancerIP": "<your-reserved-static-ip>" }}'
```

### Step 2: Verify static IP address of knative-ingressgateway service

You can check the external IP of the "knative-ingressgateway" service with:

```shell
kubectl get svc knative-ingressgateway -n istio-system
```

The result should be something like

```console
NAME                     TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                                      AGE
knative-ingressgateway   LoadBalancer   10.50.250.120   35.210.48.100   80:32380/TCP,443:32390/TCP,32400:32400/TCP   5h
```

The external IP will be eventually set to the static IP. This process could
take several minutes.
