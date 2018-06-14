# Telemetry Sample

This sample runs a simple web server that makes calls to other in-cluster services
and responds to requests with "Hello World!".
The purpose of this sample is to show generating metrics, logs and distributed traces
(see [Logs and Metrics](../../docs/telemetry.md) for more information).
This sample also creates a dedicated Prometheus instances rather than using the one
that is installed by default as a showcase of installing dedicated Prometheus instances.

## Prerequisites

1. [Install Knative Serving](https://github.com/knative/install/blob/master/README.md)
2. [Install Knative monitoring component](docs/telemetry.md)
3. Install [docker](https://www.docker.com/)


## Setup

Build the app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=telemetrysample \
  --tag "${REPO}/sample/telemetrysample" \
  --file=sample/Dockerfile.golang .
docker push "${REPO}/sample/telemetrysample"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/knative/serving/sample/telemetrysample@${REPO}/sample/telemetrysample@g" sample/telemetrysample/*.yaml

# Deploy the Knative Serving sample
kubectl apply -f sample/telemetrysample/
```

## Exploring

Once deployed, you can inspect the created resources with `kubectl` commands:

```shell
# This will show the route that we created:
kubectl get route -o yaml

# This will show the configuration that we created:
kubectl get configurations -o yaml

# This will show the Revision that was created by our configuration:
kubectl get revisions -o yaml
```

To access this service via `curl`, we first need to determine its ingress address:
```shell
watch kubectl get ingress
NAME                                 HOSTS                     ADDRESS   PORTS     AGE
telemetrysample-route-ingress   telemetrysample.myhost.net             80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route telemetrysample-route -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress telemetrysample-route-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`

# Curl the Ingress IP "as-if" DNS were properly configured.
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
Hello World!
```

Generate some logs to STDOUT and files under `/var/log` in `Json` or plain text formats.

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/log
Sending logs done.
```

## Accessing logs
You can access to the logs from Kibana UI - see [Logs and Metrics](../../docs/telemetry.md) for more information.

## Accessing per request traces
You can access to per request traces from Zipkin UI - see [Logs and Metrics](../../docs/telemetry.md) for more information.

## Accessing custom metrics
You can see published metrics using Prometheus UI. To access to the UI, forward the Prometheus server to your machine:

```bash
kubectl port-forward $(kubectl get pods --selector=app=prometheus,prometheus=test --output=jsonpath="{.items[0].metadata.name}") 9090
```

Then browse to http://localhost:9090.

## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/telemetrysample/
```
