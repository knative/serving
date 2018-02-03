# Sample Function

A trivial function for HTTP events, which returns `"Hello {name}"`, for a
querystring parameter `name`.

This is based on the source code available from: github.com/steren/sample-function

## Prerequisites

1. [Setup your development environment](../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../README.md#start-elafros)

## Running

You can deploy this to Elafros from the root directory via:
```shell
$ bazel run sample/steren-function:everything.create
INFO: Analysed target //sample/steren-function:everything.create (1 packages loaded).
INFO: Found 1 target...
Target //sample/steren-function:everything.create up-to-date:
  bazel-bin/sample/steren-function/everything.create
INFO: Elapsed time: 0.634s, Critical Path: 0.07s
INFO: Build completed successfully, 4 total actions

INFO: Running command line: bazel-bin/sample/steren-function/everything.create
buildtemplate "node-fn" created
elaservice "steren-sample-fn" created
revisiontemplate "steren-sample-function" created
```

Once deployed, you will see that it first builds:

```shell
$ kubectl get revision -o yaml
apiVersion: v1
items:
- apiVersion: elafros.dev/v1alpha1
  kind: Revision
  ...
  status:
    conditions:
    - reason: Building
      status: "False"
      type: BuildComplete
...
```

Once the `BuildComplete` status becomes `True` the resources will start getting created.


To access this service via `curl`, we first need to determine its ingress address:
```shell
$ watch $ kubectl get ing
NAME                           HOSTS                          ADDRESS    PORTS     AGE
steren-sample-fn-ela-ingress   sample-fn.googlecustomer.net              80        3m
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress IP into an environment variable.
$ export SERVICE_IP=`kubectl get ingress steren-sample-fn-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`

# Curl the Ingress IP "as-if" DNS were properly configured.
$ curl --header 'Host:sample-fn.googlecustomer.net' http://${SERVICE_IP}/execute?name=${USER}
Hello mattmoor
```

## Cleaning up

To clean up the sample service:

```shell
bazel run sample/steren-function:everything.delete
```
