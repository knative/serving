# Setting Up Static IP for Cluster Ingresses
All Ingresses in Istio service mesh share the same IP address, which is the 
external IP address of service "istio-ingress" under namespace "istio-system". So to set static IP for Ingresses, you just need to set the external IP of service "istio-ingress" to the static IP you need.

## Step 1: Reserve a static IP
### Knative on GKE
If you are running Knative cluster on GKE, you can follow the [instruction](https://cloud.google.com/compute/docs/ip-addresses/reserve-static-external-ip-address#reserve_new_static) to reserve a REGIONAL 
IP address. The region of the IP address should be the region your Knative
 cluster is running on (e.g. us-east1, us-central1, etc.).

TODO: find out how to reserve static IP in other cloud platforms.

## Step 2: Update istio.yaml
Set the loadBalancerIP of "istio-ingress" service by adding the following 
patch into the spec of "istio-ingress" service in [istio.yaml](https://github.com/knative/serving/blob/master/third_party/istio-0.8.0/istio.yaml#L1999)
```
loadBalancerIP: <your-static-ip-address>
```

## Step 3: Verify static IP address of Ingress
Follow the [instruction](https://github.com/knative/serving/blob/master/DEVELOPMENT.md#deploy-istio) to deploy Istio.
You can check the external IP of "istio-ingress" service with command
```
kubectl get svc istio-ingress -n istio-system
```
The result should be something like
```
NAME            TYPE           CLUSTER-IP      EXTERNAL-IP      PORT(S)                      AGE
istio-ingress   LoadBalancer   10.59.251.183   35.231.181.148   80:32000/TCP,443:31270/TCP   9h
```

The external IP in the result should be the same as the static IP you set.
