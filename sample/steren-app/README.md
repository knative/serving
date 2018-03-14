# Sample App

A web application that accepts names, storing them in Google Cloud Datastore, and
lists all of the names it has seen.

This is based on the source code available from: github.com/steren/sample-app

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)
3. Enable the Google Cloud Datastore API.

## Running

You can deploy this to Elafros from the root directory via:
```shell
$ bazel run sample/steren-app:everything.create
INFO: Analysed target //sample/steren-app:everything.create (1 packages loaded).
INFO: Found 1 target...
Target //sample/steren-app:everything.create up-to-date:
  bazel-bin/sample/steren-app/everything.create
INFO: Elapsed time: 0.634s, Critical Path: 0.07s
INFO: Build completed successfully, 4 total actions

INFO: Running command line: bazel-bin/sample/steren-app/everything.create
buildtemplate "node-app" created
route "steren-sample-app" created
configuration "steren-sample-app" created
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
$ watch kubectl get ing
NAME                             HOSTS                          ADDRESS    PORTS     AGE
steren-sample-app-ela-ingress   steren-sample-app.default.demo-domain.net              80        3m
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get ingress steren-sample-app-ela-ingress -o jsonpath="{.spec.rules[*].host}"`

# Put the Ingress IP into an environment variable.
$ export SERVICE_IP=`kubectl get ingress steren-sample-app-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your ingress will never get an address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l istio=ingress -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

Now curl the service IP as if DNS were properly configured:

```shell
$ curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/
<!DOCTYPE html><html><head><title>Demo</title><link rel="stylesheet" href="/stylesheets/style.css"></head><body><h1>Demo</h1><form action="/messages" method="POST"><input type="text" name="text"><input type="submit"></form><ol></ol></body></html>
```

## Cleaning up

To clean up the sample service:

```shell
bazel run sample/steren-app:everything.delete
```
