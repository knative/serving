# Telemetry Sample

This sample runs a simple web server that makes calls to other in-cluster services
and responds to requests with "Hello World!".
The purpose of this sample is to show generating metrics, logs and distributed traces
(see [Logs and Metrics](../../docs/telemetry.md) for more information). 
This sample also creates a dedicated Prometheus instances rather than using the one
that is installed by default as a showcase of installing dedicated Prometheus instances.

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)

## Running

Deploy the sample via:
```shell
bazel run sample/telemetrysample:everything.apply
```

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
telemetrysample-route-ela-ingress   telemetrysample.myhost.net             80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress telemetrysample-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`

# Curl the Ingress IP "as-if" DNS were properly configured.
curl --header 'Host:telemetrysample.myhost.net' http://${SERVICE_IP}
Hello World!
```

## Accessing logs
You can access to the logs from Kibana UI - see [Logs and Metrics](../../docs/telemetry.md) for more information.

## Accessing per request traces
You can access to per request traces from Zipkin UI - see [Logs and Metrics](../../docs/telemetry.md) for more information.

## Accessing metrics on the dedicated Prometheus instance
First, get the service IP that Prometheus UI is exposed on:

```shell
# Put the Ingress IP into an environment variable.
kubectl get service prometheus-test-web -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"

104.154.90.16
```

Copy the IP address and browse to <IP_ADDRESS>:30800.

## Cleaning up

To clean up the sample service:

```shell
bazel run sample/telemetrysample:everything.delete
```
