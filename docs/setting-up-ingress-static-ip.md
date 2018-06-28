# Setting Up Static IP for Cluster Ingresses

All Ingresses in Istio service mesh share the same IP address, which is the 
external IP address of the "istio-ingress" service under the "istio-system" 
namespace. So to set static IP for Ingresses, you just need to set the 
external IP address of the "istio-ingress" service to the static IP you need.

## Prerequisites

### Prerequisite 1: Reserve a static IP

#### Knative on GKE

If you are running Knative cluster on GKE, you can follow the [instructions](https://cloud.google.com/compute/docs/ip-addresses/reserve-static-external-ip-address#reserve_new_static) to reserve a REGIONAL 
IP address. The region of the IP address should be the region your Knative
 cluster is running in (e.g. us-east1, us-central1, etc.).

TODO: add documentation on reserving static IP in other cloud platforms.

### Prerequisite 2: Deploy Istio

Follow the [instructions](https://github.com/knative/serving/blob/master/DEVELOPMENT.md#deploy-istio) 
to deploy Istio into your cluster.

Once you reach this point, you can start to set up static IP for Ingresses.

## Set Up Static IP for Ingresses

### Step 1: Update external IP of istio-ingress service

Run following command to reset the external IP for the "istio-ingress" service 
to the static IP you reserved.
```shell
kubectl patch svc istio-ingress -n istio-system --patch '{"spec": { "loadBalancerIP": "<your-reserved-static-ip>" }}'
```

### Step 2: Verify static IP address of Ingresses

You can check the external IP of the "istio-ingress" service with:
```shell
kubectl get svc istio-ingress -n istio-system
```
The result should be something like
```
NAME            TYPE           CLUSTER-IP      EXTERNAL-IP      PORT(S)                      AGE
istio-ingress   LoadBalancer   10.59.251.183   35.231.181.148   80:32000/TCP,443:31270/TCP   9h
```
The external IP will be eventually set to the static IP. This process could 
take several minutes.