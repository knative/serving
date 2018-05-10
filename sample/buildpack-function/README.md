# Buildpack Sample Function

A sample function that demonstrates usage of Cloud Foundry buildpacks on
Elafros, using the [packs Docker images](https://github.com/sclevine/packs).

This deploys the [riff square](https://github.com/socthis/riff-square-buildpack)
sample function for riff.

## Prerequisites

[Install Elafros](https://github.com/elafros/install/blob/master/README.md)

## Running

You can deploy this to Elafros from the root directory via:
```shell
# Replace the token string with a suitable registry
REPO="gcr.io/<your-project-here>"
perl -pi -e "s@DOCKER_REPO_OVERRIDE@$REPO@g" sample/buildpack-function/sample.yaml

# Create the Kubernetes resources
kubectl apply -f sample/templates/buildpack.yaml -f sample/buildpack-function/sample.yaml
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
NAME                             HOSTS                                        ADDRESS   PORTS   AGE
buildpack-function-ela-ingress   buildpack-function.default.demo-domain.com   0.0.0.0   80      3m
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
$ export SERVICE_HOST=`kubectl get route buildpack-function -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
$ export SERVICE_IP=`kubectl get ingress buildpack-function-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`

# Curl the Ingress IP "as-if" DNS were properly configured.
$ curl http://${SERVICE_IP}/ -H "Host: $SERVICE_HOST" -H "Content-Type: application/json" -d "33"
[response]
```

## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/buildpack-function/sample.yaml
```
