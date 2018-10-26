# Creating a Kubernetes Cluster for Knative Serving

Two options:

* Setup a [GKE cluster](#gke)
* Run [minikube](#minikube) locally

## GKE

To use a k8s cluster running in GKE:

1. Install `gcloud` using [the instructions for your
    platform](https://cloud.google.com/sdk/downloads).

1. Create a GCP project (or use an existing project if you've already created
    one) at http://console.cloud.google.com/home/dashboard. Set the ID of the
    project in an environment variable (e.g. `PROJECT_ID`).

    _If you are a new GCP user, you might be eligible for a trial credit making
    your GKE cluster and other resources free for a short time. Otherwise, any
    GCP resources you create will cost money._

1. Enable the k8s API:

    ```shell
    gcloud --project=$PROJECT_ID services enable container.googleapis.com
    ```

1. Create a k8s cluster (version 1.10 or greater):

    ```shell
    gcloud --project=$PROJECT_ID container clusters create \
      --cluster-version=latest \
      --zone=us-east1-d \
      --scopes=cloud-platform \
      --machine-type=n1-standard-4 \
      --enable-autoscaling --min-nodes=1 --max-nodes=3 \
      knative-demo
    ```

    * Version 1.10+ is required
    * Change this to whichever zone you choose
    * cloud-platform scope is required to access GCB
    * Knative Serving currently requires 4-cpu nodes to run conformance tests.
        Changing the machine type from the default may cause failures.
    * Autoscale from 1 to 3 nodes. Adjust this for your use case
    * Change this to your preferred cluster name

    You can see the list of supported cluster versions in a particular zone by
    running:

    ```shell
    # Get the list of valid versions in us-east1-d
    gcloud container get-server-config --zone us-east1-d
    ```

1. **Alternately**, if you wish to re-use an already-created cluster,
    you can fetch the credentials to your local machine with:

    ```shell
    # Load credentials for the new cluster in us-east1-d
    gcloud container clusters get-credentials --zone us-east1-d knative-demo
    ```

1. If you haven't installed `kubectl` yet, you can install it now with `gcloud`:

    ```shell
    gcloud components install kubectl
    ```

1. Add to your .bashrc:

    ```shell
    # When using GKE, the K8s user is your GCP user.
    export K8S_USER_OVERRIDE=$(gcloud config get-value core/account)
    ```

## Minikube

1. [Install and configure
    minikube](https://github.com/kubernetes/minikube#minikube) version v0.28.1 or later with a [VM
    driver](https://github.com/kubernetes/minikube#requirements), e.g. `kvm2` on
    Linux or `hyperkit` on macOS.

1. [Create a cluster](https://github.com/kubernetes/minikube#quickstart) with
    version 1.10 or greater and your chosen VM driver.

    The following commands will setup a cluster with `8192 MB` of memory and `4`
    CPUs. If you want to [enable metric and log collection](./DEVELOPMENT.md#enable-log-and-metric-collection),
    bump the memory to `12288 MB`.

    _Providing any admission control pluins overrides the default set provided
    by minikube so we must explicitly list all plugins we want enabled._

    _Until minikube [makes this the
    default](https://github.com/kubernetes/minikube/issues/1647), the
    certificate controller must be told where to find the cluster CA certs on
    the VM._

    For Linux use:

    ```shell
    minikube start --memory=8192 --cpus=4 \
    --kubernetes-version=v1.11.3 \
    --vm-driver=kvm2 \
    --bootstrapper=kubeadm \
    --extra-config=apiserver.enable-admission-plugins="LimitRanger,NamespaceExists,NamespaceLifecycle,ResourceQuota,ServiceAccount,DefaultStorageClass,MutatingAdmissionWebhook"
    ```

    For macOS use:

    ```shell
    minikube start --memory=8192 --cpus=4 \
    --kubernetes-version=v1.11.3 \
    --vm-driver=hyperkit \
    --bootstrapper=kubeadm \
    --extra-config=apiserver.enable-admission-plugins="LimitRanger,NamespaceExists,NamespaceLifecycle,ResourceQuota,ServiceAccount,DefaultStorageClass,MutatingAdmissionWebhook"
    ```

1. [Configure your shell environment](../DEVELOPMENT.md#environment-setup)
    to use your minikube cluster:

    ```shell
    export K8S_CLUSTER_OVERRIDE='minikube'
    # When using Minikube, the K8s user is your local user.
    export K8S_USER_OVERRIDE=$USER
    ```

1. Install Knative Serving

    Before installing knative on minikube, we need to do two things:

    1. [Workaround Minikube's lack of support for `LoadBalancer` type services](#loadbalancer-support-in-minikube)
    1. [Configure `ko` for local publishing](#minikube-with-ko)

    After doing those, you can [deploy the Knative Serving
    components](../DEVELOPMENT.md#starting-knative-serving) to
    Minikube the same way you would any other Kubernetes cluster.

### `LoadBalancer` Support in Minikube

By default istio uses a `LoadBalancer` which is [not yet supported by
Minikube](https://github.com/kubernetes/minikube/issues/2834) but is
required for Knative to function properly (`Route` endpoints MUST be
available via a routable IP before they will be marked as ready). [One
possible
workaround](https://github.com/elsonrodriguez/minikube-lb-patch) is to
install a custom controller that provisions an external IP using the
service's ClusterIP, which must be made routable on the minikube host.
These two commands accomplish this, and should be run once whenever
you start a new minikube cluster:

```bash
sudo ip route add $(cat ~/.minikube/profiles/minikube/config.json | jq -r ".KubernetesConfig.ServiceCIDR") via $(minikube ip)
kubectl run minikube-lb-patch --replicas=1 --image=elsonrodriguez/minikube-lb-patch:0.1 --namespace=kube-system
```

### Minikube with `ko`

You can instruct `ko` to sideload images into your Docker daemon
instead of publishing them to a registry by setting
`KO_DOCKER_REPO=ko.local`:

```shell
# Use the minikube docker daemon (among other things)
eval $(minikube docker-env)

# Switch the current kubectl context to minikube
kubectl config use-context minikube

# Set KO_DOCKER_REPO to a sentinel value for ko to sideload into the daemon.
export KO_DOCKER_REPO="ko.local"
```

### Enabling Knative to Use Images in Minikube

In order to have Knative access an image in Minikube's Docker daemon you
should prefix your image name with the `dev.local` registry. This will cause
Knative to use the cached image. You must not tag your image as `latest` since
this causes Kubernetes to [always attempt a pull](https://kubernetes.io/docs/concepts/containers/images/#updating-images).

For example:

```shell
eval $(minikube docker-env)
docker pull gcr.io/knative-samples/primer:latest
docker tag gcr.io/knative-samples/primer:latest dev.local/knative-samples/primer:v1
```

### Minikube with GCR

You can use Google Container Registry as the registry for a Minikube cluster.

1. [Set up a GCR repo](setting-up-a-docker-registry.md). Export the environment
    variable `PROJECT_ID` as the name of your project. Also export `GCR_DOMAIN`
    as the domain name of your GCR repo. This will be either `gcr.io` or a
    region-specific variant like `us.gcr.io`.

    ```shell
    export PROJECT_ID=knative-demo-project
    export GCR_DOMAIN=gcr.io
    ```

    To publish builds push to GCR, set `KO_DOCKER_REPO` or
    `DOCKER_REPO_OVERRIDE` to the GCR repo's url.

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

For example, use these steps to allow Minikube to pull Knative Serving and Build images
from GCR as published in our development flow (`ko apply -f config/`).
_This is only necessary if you are not using public Knative Serving and Build images._

1. Create a Kubernetes secret in the `knative-serving` and `knative-build` namespace:

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

See also the [private-repo sample README](./../sample/private-repos/README.md).
