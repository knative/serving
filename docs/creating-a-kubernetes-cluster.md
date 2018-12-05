# Creating a Kubernetes cluster

This doc describes two options for creating a k8s cluster:

- Setup a [GKE cluster](#gke)
- Run [minikube](#minikube) locally

## GKE

1. [Install required tools and setup GCP project](https://github.com/knative/docs/blob/master/install/Knative-with-GKE.md#before-you-begin)
   (You may find it useful to save the ID of the project in an environment
   variable (e.g. `PROJECT_ID`)).
1. [Create a GKE cluster for knative](https://github.com/knative/docs/blob/master/install/Knative-with-GKE.md#creating-a-kubernetes-cluster)

_If you are a new GCP user, you might be eligible for a trial credit making your
GKE cluster and other resources free for a short time. Otherwise, any GCP
resources you create will cost money._

If you have an existing GKE cluster you'd like to use, you can fetch your
credentials with:

```shell
# Load credentials for the new cluster in us-east1-d
gcloud container clusters get-credentials --zone us-east1-d knative-demo
```

## Minikube

1. [Install required tools](https://github.com/knative/docs/blob/master/install/Knative-with-Minikube.md#before-you-begin)
1. [Create a Kubernetes cluster with minikube](https://github.com/knative/docs/blob/master/install/Knative-with-Minikube.md#creating-a-kubernetes-cluster)
1. [Configure your shell environment](../DEVELOPMENT.md#environment-setup) to
   use your minikube cluster:

   ```shell
   export K8S_CLUSTER_OVERRIDE='minikube'
   ```

1. Take note of the workarounds required for:

   - [Installing Istio](https://github.com/knative/docs/blob/master/install/Knative-with-Minikube.md#installing-istio)
   - [Installing Serving](https://github.com/knative/docs/blob/master/install/Knative-with-Minikube.md#installing-knative-serving)
   - [Loadbalancer support](#loadbalancer-support-in-minikube)
   - [`ko`](#minikube-with-ko)
   - [Images](#enabling-knative-to-use-images-in-minikube)
   - [GCR](#minikube-with-gcr)

### `LoadBalancer` Support in Minikube

By default istio uses a `LoadBalancer` which is
[not yet supported by Minikube](https://github.com/kubernetes/minikube/issues/2834)
but is required for Knative to function properly (`Route` endpoints MUST be
available via a routable IP before they will be marked as ready).
[One possible workaround](https://github.com/elsonrodriguez/minikube-lb-patch)
is to install a custom controller that provisions an external IP using the
service's ClusterIP, which must be made routable on the minikube host. These two
commands accomplish this, and should be run once whenever you start a new
minikube cluster:

```bash
sudo ip route add $(cat ~/.minikube/profiles/minikube/config.json | jq -r ".KubernetesConfig.ServiceCIDR") via $(minikube ip)
kubectl run minikube-lb-patch --replicas=1 --image=elsonrodriguez/minikube-lb-patch:0.1 --namespace=kube-system
```

### Minikube with `ko`

You can instruct `ko` to sideload images into your Docker daemon instead of
publishing them to a registry by setting `KO_DOCKER_REPO=ko.local`:

```shell
# Use the minikube docker daemon (among other things)
eval $(minikube docker-env)

# Switch the current kubectl context to minikube
kubectl config use-context minikube

# Set KO_DOCKER_REPO to a sentinel value for ko to sideload into the daemon.
export KO_DOCKER_REPO="ko.local"
```

### Enabling Knative to Use Images in Minikube

In order to have Knative access an image in Minikube's Docker daemon you should
prefix your image name with the `dev.local` registry. This will cause Knative to
use the cached image. You must not tag your image as `latest` since this causes
Kubernetes to
[always attempt a pull](https://kubernetes.io/docs/concepts/containers/images/#updating-images).

For example:

```shell
eval $(minikube docker-env)
docker pull gcr.io/knative-samples/primer:latest
docker tag gcr.io/knative-samples/primer:latest dev.local/knative-samples/primer:v1
```

### Minikube with GCR

You can use Google Container Registry as the registry for a Minikube cluster.

1. [Set up a GCR repo](docs/setting-up-a-docker-registry.md). Export the
   environment variable `PROJECT_ID` as the name of your project. Also export
   `GCR_DOMAIN` as the domain name of your GCR repo. This will be either
   `gcr.io` or a region-specific variant like `us.gcr.io`.

   ```shell
   export PROJECT_ID=knative-demo-project
   export GCR_DOMAIN=gcr.io
   ```

   To publish builds push to GCR, set `KO_DOCKER_REPO` or `DOCKER_REPO_OVERRIDE`
   to the GCR repo's url.

   ```shell
   export KO_DOCKER_REPO="${GCR_DOMAIN}/${PROJECT_ID}"
   export DOCKER_REPO_OVERRIDE="${KO_DOCKER_REPO}"
   ```

1. Create a GCP service account:

   ```shell
   gcloud iam service-accounts create minikube-gcr \
     --display-name "Minikube GCR Pull" \
     --project $PROJECT_ID
   ```

1. Give your service account the `storage.objectViewer` role:

   ```shell
   gcloud projects add-iam-policy-binding $PROJECT_ID \
     --member "serviceAccount:minikube-gcr@${PROJECT_ID}.iam.gserviceaccount.com" \
     --role roles/storage.objectViewer
   ```

1. Create a key credential file for the service account:

   ```shell
   gcloud iam service-accounts keys create \
     --iam-account "minikube-gcr@${PROJECT_ID}.iam.gserviceaccount.com" \
     minikube-gcr-key.json
   ```

Now you can use the `minikube-gcr-key.json` file to create image pull secrets
and link them to Kubernetes service accounts. _A secret must be created and
linked to a service account in each namespace that will pull images from GCR._

For example, use these steps to allow Minikube to pull Knative Serving and Build
images from GCR as published in our development flow (`ko apply -f config/`).
_This is only necessary if you are not using public Knative Serving and Build
images._

1. Create a Kubernetes secret in the `knative-serving` and `knative-build`
   namespace:

   ```shell
   export DOCKER_EMAIL=your.email@here.com
   kubectl create secret docker-registry "knative-serving-gcr" \
     --docker-server=$GCR_DOMAIN \
     --docker-username=_json_key \
     --docker-password="$(cat minikube-gcr-key.json)" \
     --docker-email=$DOCKER_EMAIL \
     -n "knative-serving"
   kubectl create secret docker-registry "build-gcr" \
     --docker-server=$GCR_DOMAIN \
     --docker-username=_json_key \
     --docker-password="$(cat minikube-gcr-key.json)" \
     --docker-email=$DOCKER_EMAIL \
     -n "knative-build"
   ```

   _The secret must be created in the same namespace as the pod or service
   account._

1. Add the secret as an imagePullSecret to the `controller` and
   `build-controller` service accounts:

   ```shell
   kubectl patch serviceaccount "build-controller" \
     -p '{"imagePullSecrets": [{"name": "build-gcr"}]}' \
     -n "knative-build"
   kubectl patch serviceaccount "controller" \
     -p '{"imagePullSecrets": [{"name": "knative-serving-gcr"}]}' \
     -n "knative-serving"
   ```

Use the same procedure to add imagePullSecrets to service accounts in any
namespace. Use the `default` service account for pods that do not specify a
service account.

See also the [private-repo sample README](/sample/private-repos/README.md).
