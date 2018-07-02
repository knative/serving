# Python Simple Sample

A simple web server written in Python that you can use for testing. It has two
handlers:

  1. One reads in an env variable 'TARGET' and prints "Hello World from Python: ${TARGET}!".
     If TARGET is not specified, it will use "NOT SPECIFIED" as the TARGET.
  1. Another refers an undefined variable and sends multi-line exception stack
     trace logs to STDERR and file under /var/log.

The server is made into a docker container and provided to Knative Serving.

## Prerequisites

1. [Install Knative Serving](https://github.com/knative/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Running

First build and push the docker image from the root directory via:
```shell
docker build -t "${DOCKER_REPO_OVERRIDE}/python-simple:latest" sample/pythonsimple/
docker push "${DOCKER_REPO_OVERRIDE}/python-simple:latest"
```

Then replace `REPLACE_ME` with the value of your `DOCKER_REPO_OVERRIDE` in
[manifest.yaml](./manifest.yaml#L36) and deploy this to Knative Serving from the root directory via:
```shell
kubectl apply -f sample/pythonsimple/manifest.yaml
```

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

To access this service via `curl`, we first need to determine the ingress address:
```shell
watch kubectl get svc knative-ingressgateway -n istio-system
```

When that service is ready, you'll see an IP address in the `EXTERNAL-IP` field:

```
NAME                     TYPE           CLUSTER-IP     EXTERNAL-IP      PORT(S)                                      AGE
knative-ingressgateway   LoadBalancer   10.23.247.74   35.203.155.229   80:32380/TCP,443:32390/TCP,32400:32400/TCP   2d
```

Once the `EXTERNAL-IP` gets assigned to the cluster, you can run:

```shell
# Put the Host name into an environment variable.
export SERVICE_HOST=`kubectl get route route-python-example -o jsonpath="{.status.domain}"`

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
# Hello World from Python: shiniestnewestversion!
```

Generate multi-line exception stack trace logs to STDERR and file under /var/log:
```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/error
# exception stack trace logs were send to stderr and /var/log/error.log
```

See [Logs and Metrics](/docs/telemetry.md) for accessing logs.
