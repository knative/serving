# Service

A simple web server that you can use for testing Service resource.
It reads in an env variable 'TARGET' and prints "Hello World: ${TARGET}!" if
TARGET is not specified, it will use "NOT SPECIFIED" as the TARGET.

## Prerequisites

1. [Install Elafros](https://github.com/knative/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Setup

Build the app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=service \
  --tag "${REPO}/sample/service" \
  --file=sample/Dockerfile.golang .
docker push "${REPO}/sample/service"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/knative/serving/sample/service@${REPO}/sample/service@g" sample/service/*.yaml

# Deploy the Elafros sample
kubectl apply -f sample/service/sample.yaml
```

## Exploring

Once deployed, you can inspect the created resources with `kubectl` commands:

```shell
# This will show the service that we created:
kubectl get service.knative.dev -oyaml
```

```shell
# This will show the route that was created by the service:
kubectl get route -o yaml
```

```shell
# This will show the configuration that was created by the service:
kubectl get configurations -o yaml
```

```shell
# This will show the Revision that was created the configuration created by the service:
kubectl get revisions -o yaml

```

To access this service via `curl`, we first need to determine its ingress address:
```shell
watch kubectl get ingress
```

When the ingress is ready, you'll see an IP address in the ADDRESS field:

```
NAME                                 HOSTS                     ADDRESS   PORTS     AGE
service-example-ela-ingress   demo.myhost.net             80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route service-example -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress service-example-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your ingress will never get an address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l istio=ingress -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

Now curl the service IP as if DNS were properly configured:

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
# Hello World: shiniestnewestversion!
```

## Updating

You can update this to a new version. For example, update it with a new service.yaml via:
```shell
kubectl apply -f sample/service/updated_service.yaml
```

Once deployed, traffic will shift to the new revision automatically. You can verify the new version
by checking route status:
```shell
# This will show the route that we created:
kubectl get route -o yaml
```

Or curling the service:
```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
# Hello World: evenshinierversion!
```

## Pinning a service

You can pin a Service to a specific revision. For example, update it with a new service.yaml via:
```shell
kubectl apply -f sample/service/pinned_service.yaml
```

Once deployed, traffic will shift to the previous (first) revision automatically. You can verify the new version
by checking route status:
```shell
# This will show the route that we created:
kubectl get route -o yaml
```

Or curling the service:
```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
# Hello World: shiniestnewestversion!
```

## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/service/sample.yaml
```
