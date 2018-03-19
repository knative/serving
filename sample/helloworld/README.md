# Helloworld

A simple web server that you can use for testing. It reads in an
env variable 'TARGET' and prints "Hello World: ${TARGET}!" if
TARGET is not specified, it will use "NOT SPECIFIED" as the TARGET.

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)

## Running

You can deploy this to Elafros from the root directory via:
```shell
bazel run sample/helloworld:everything.create
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
route-example-ela-ingress   demo.myhost.net             80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route route-example -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress route-example-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your ingress will never get an address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l istio=ingress -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

Now curl the service IP as if DNS were properly configured:

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
Hello World: shiniestnewestversion!
```

## Updating

You can update this to a new version. For example, update it with a new configuration.yaml via:
```shell
bazel run sample/helloworld:updated_everything.apply
```

Once deployed, traffic will shift to the new revision automatically. You can verify the new version
by checking route status:
```shell
# This will show the route that we created:
kubectl get route -o yaml
```

Or curling the service:
```
$ curl --header 'Host:$SERVICE_HOST http://${SERVICE_IP}
Hello World: nextversion!
```

## Manual traffic splitting

You can manually split traffic to specific revisions. Get your revisions names via:
```shell
$ kubectl get revisions
NAME                                     AGE
p-1552447d-0690-4b15-96c9-f085e310e98d   22m
p-30e6a938-b28b-4d5e-a791-2cb5fe016d74   10m
```

Update `traffic` part in sample/helloworld/route.yaml as:
```yaml
traffic:
  - revisionName: <YOUR_FIRST_REVISION_NAME>
    percent: 50
  - revisionName: <YOUR_SECOND_REVISION_NAME>
    percent: 50
```

Then update your change via:
```shell
bazel run sample/helloworld:everything.apply
```

Once updated, you can verify the traffic splitting by looking at route status and/or curling
the service.

## Cleaning up

To clean up the sample service:

```shell
bazel run sample/helloworld:everything.delete
```
