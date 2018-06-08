# Buildpack Sample App

A sample app that demonstrates usage of Cloud Foundry buildpacks on Knative Serving,
using the [packs Docker images](https://github.com/sclevine/packs).

This deploys the [.NET Core Hello World](https://github.com/cloudfoundry-samples/dotnet-core-hello-world)
sample app for Cloud Foundry.

## Prerequisites

[Install Knative Serving](https://github.com/knative/install/blob/master/README.md)

## Running

This sample uses the [Buildpack build
template](https://github.com/knative/build-templates/blob/master/buildpack/buildpack.yaml)
in the [build-templates](https://github.com/knative/build-templates/) repo.

First, install the Buildpack build template from that repo:

```shell
kubectl apply -f buildpack.yaml
```

Then you can deploy this to Knative Serving from the root directory via:

```shell
# Replace the token string with a suitable registry
REPO="gcr.io/<your-project-here>"
perl -pi -e "s@DOCKER_REPO_OVERRIDE@$REPO@g" sample/buildpack-app/sample.yaml

# Create the Kubernetes resources
kubectl apply -f sample/buildpack-app/sample.yaml
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
NAME                             HOSTS                          ADDRESS    PORTS     AGE
buildpack-sample-app-ingress buildpack-app.example.com                 80        3m
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route buildpack-sample-app -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
$ export SERVICE_IP=`kubectl get ingress buildpack-sample-app-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`

# Curl the Ingress IP "as-if" DNS were properly configured.
$ curl --header "Host: $SERVICE_HOST" http://${SERVICE_IP}/
[response]
```

## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/templates/buildpack.yaml -f sample/buildpack-app/sample.yaml
```
