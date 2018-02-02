# Helloworld

A simple web server that you can use for testing. It reads in an
env variable 'TARGET' and prints "Hello World: ${TARGET}!" if
TARGET is not specified, it will use "NOT SPECIFIED" as the TARGET.

## Prerequisites

1. [Setup your development environment](../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../README.md#start-elafros)

## Running

You can deploy this to Elafros from the root directory via:
```shell
bazel run sample/helloworld:everything.create
```

Once deployed, you can inspect the created resources with `kubectl` commands:

```shell
# This will show the ES that we created:
kubectl get elaservice -o yaml

# This will show the RT that we created:
kubectl get revisiontemplates -o yaml

# This will show the Revision that was created by our RT:
kubectl get revisions -o yaml

```

To access this service via `curl`, we first need to determine its ingress address:
```shell
$ watch kubectl get ingress
NAME                            HOSTS             ADDRESS         PORTS     AGE
elaservice-example-ela-ingress  demo.myhost.net   35.227.55.157   80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress IP into an environment variable.
$ export SERVICE_IP=`kubectl get ingress elaservice-example-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`

# Curl the Ingress IP "as-if" DNS were properly configured.
$ curl --header 'Host:demo.myhost.net' http://${SERVICE_IP}
Hello World: shiniestnewestversion!
```

## Cleaning up

To clean up the sample service:

```shell
bazel run sample/helloworld:everything.delete
```
