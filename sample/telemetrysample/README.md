# Telemetry Sample

A simple web server that you can use for testing. When called, the
server waits randomly up to 1 second and emmits metrics that you can 
see in Prometheus.

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)

## Running

You can deploy this to Elafros from the root directory via:
```shell
bazel run sample/telemetrysample:everything.create
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
$ watch kubectl get ingress
NAME                                 HOSTS                     ADDRESS   PORTS     AGE
telemetrysample-route-ela-ingress   telemetrysample.myhost.net             80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress IP into an environment variable.
$ export SERVICE_IP=`kubectl get ingress telemetrysample-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`

# Curl the Ingress IP "as-if" DNS were properly configured.
$ curl --header 'Host:telemetrysample.myhost.net' http://${SERVICE_IP}
Hello World!
```

## Checking Metrics

First, get the service IP that Prometheus UI is exposed on:

```shell
# Put the Ingress IP into an environment variable.
$ kubectl get service prometheus-test-web -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"

104.154.90.16
```

Copy the IP address and browse to <IP_ADDRESS>:30800.

## Cleaning up

To clean up the sample service:

```shell
bazel run sample/telemetrysample:everything.delete
```
