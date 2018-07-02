# Stock

A simple Restful service for testing purpose. It expose an endpoint, which takes
a stock ticker (stock symbol) and output the stock price. It uses the the REST resource
name from environment defined in configuration.

## Prerequisites

1. [Install Knative Serving](https://github.com/knative/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Setup

Build the app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=stock-rest-app \
  --tag "${REPO}/sample/stock-rest-app" \
  --file=sample/Dockerfile.golang .
docker push "${REPO}/sample/stock-rest-app"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/knative/serving/sample/stock-rest-app@${REPO}/sample/stock-rest-app@g" sample/stock-rest-app/*.yaml

# Deploy the Knative Serving sample
kubectl apply -f sample/stock-rest-app/sample.yaml
```

## Exploring

Once deployed, you can inspect the created resources with `kubectl` commands:

```shell
# This will show the route that we created:
kubectl get route -o yaml
```

```shell
# This will show the configuration that we created:
kubectl get configurations -o yaml
```

```shell
# This will show the Revision that was created by our configuration:
kubectl get revisions -o yaml

```

To access this service via `curl`, we first need to determine its ingress address:
```shell
watch get svc knative-ingressgateway -n istio-system
```

When the service is ready, you'll see an IP address in the EXTERNAL-IP field:

```
NAME                     TYPE           CLUSTER-IP     EXTERNAL-IP      PORT(S)                                      AGE
knative-ingressgateway   LoadBalancer   10.23.247.74   35.203.155.229   80:32380/TCP,443:32390/TCP,32400:32400/TCP   2d
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the host name into an environment variable.
export SERVICE_HOST=`kubectl get route stock-route-example -o jsonpath="{.status.domain}"`

# Put the ingress IP into an environment variable.
export SERVICE_IP=`kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your services will never get an external IP address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l knative=ingressgateway -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc knative-ingressgateway -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

Now curl the service IP as if DNS were properly configured:

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
# Welcome to the stock app!
```

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/stock
# stock ticker not found!, require /stock/{ticker}
```

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/stock/<ticker>
# stock price for ticker <ticker>  is  <price>
```

## Updating

You can update this to a new version. For example, update it with a new configuration.yaml via:
```shell
kubectl apply -f sample/stock-rest-app/updated_configuration.yaml
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
# Welcome to the share app!
```

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/share
# share ticker not found!, require /share/{ticker}
```

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/share/<ticker>
# share price for ticker <ticker>  is  <price>
```

## Manual traffic splitting

You can manually split traffic to specific revisions. Get your revisions names via:
```shell

kubectl get revisions
```

```
NAME                                AGE
stock-configuration-example-00001   11m
stock-configuration-example-00002   4m
```

Update `traffic` part in [sample/stock-rest-app/sample.yaml](./sample.yaml) as:
```yaml
traffic:
  - revisionName: <YOUR_FIRST_REVISION_NAME>
    percent: 50
  - revisionName: <YOUR_SECOND_REVISION_NAME>
    percent: 50
```

Then update your change via:
```shell
kubectl apply -f sample/stock-rest-app/sample.yaml
```

Once updated, you can verify the traffic splitting by looking at route status and/or curling
the service.

## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/stock-rest-app/sample.yaml
```
