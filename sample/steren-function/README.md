# Sample Function

A trivial function for HTTP events, which returns `"Hello {name}"`, for a
querystring parameter `name`.

This is based on the source code available from: github.com/steren/sample-function

## Prerequisites

[Install Knative Serving](https://github.com/knative/install/blob/master/README.md)

## Running

You can deploy this to Knative Serving from the root directory via:
```shell
# Replace the token string with a suitable registry
REPO="gcr.io/<your-project-here>"
perl -pi -e "s@DOCKER_REPO_OVERRIDE@$REPO@g" sample/steren-function/sample.yaml

# Create the Kubernetes resources
kubectl apply -f sample/templates/node-fn.yaml -f sample/steren-function/sample.yaml
```

Once deployed, you will see that it first builds:

```shell
$ kubectl get revision -o yaml
apiVersion: v1
items:
- apiVersion: serving.knative.dev/v1alpha1
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
NAME                           HOSTS                                      ADDRESS    PORTS     AGE
steren-sample-fn-ingress   steren-sample-fn.default.demo-domain.net              80        3m
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route steren-sample-fn -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress steren-sample-fn-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your ingress will never get an address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l istio=ingress -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

Now curl the service IP as if DNS were properly configured:

```shell
$ curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}/execute?name=${USER}
Hello mattmoor
```

## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/templates/node-fn.yaml -f sample/steren-function/sample.yaml
```
