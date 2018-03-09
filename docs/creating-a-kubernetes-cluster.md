# Creating a Kubernetes Cluster for Elafros

Two options:

*   Setup a [GKE cluster](#gke)
*   Run [minikube](#minikube) locally

## GKE

To use a k8s cluster running in GKE:

1.  Install `gcloud` using [the instructions for your
    platform](https://cloud.google.com/sdk/downloads).

1.  Create a GCP project (or use an existing project if you've already created
    one) at http://console.cloud.google.com/home/dashboard. Set the ID of the
    project in an environment variable (e.g. `PROJECT_ID`).

1.  Enable the k8s API:

    ```shell
    gcloud --project=$PROJECT_ID services enable container.googleapis.com
    ```

1.  Create a k8s cluster (version 1.9 or greater):

    ```shell
    gcloud --project=$PROJECT_ID container clusters create \
      --cluster-version=1.9.2-gke.1 \
      --zone=us-east1-d \
      --scopes=cloud-platform \
      --enable-autoscaling --min-nodes=1 --max-nodes=3 \
      elafros-demo
    ```

    *   Version 1.9+ is required
    *   Change this to whichever zone you choose
    *   cloud-platform scope is required to access GCB
    *   Autoscale from 1 to 3 nodes. Adjust this for your use case
    *   Change this to your preferred cluster name

    You can see the list of supported cluster versions in a particular zone by
    running:

    ```shell
    # Get the list of valid versions in us-east1-d
    gcloud container get-server-config --zone us-east1-d
    ```

1.  If you haven't installed `kubectl` yet, you can install it now with
    `gcloud`:

    ```shell
    gcloud components install kubectl
    ```

1.  Give your gcloud user cluster-admin privileges:

    ```shell
    kubectl create clusterrolebinding gcloud-admin-binding \
      --clusterrole=cluster-admin \
      --user=$(gcloud config get-value core/account)
    ```

1.  Add to your .bashrc:
    ```shell
    # When using GKE, the K8s user is your GCP user.
    export K8S_USER_OVERRIDE=$(gcloud config get-value core/account)
    ```

## Minikube

1.  [Install and configure
    minikube](https://github.com/kubernetes/minikube#minikube) with a [VM
    driver](https://github.com/kubernetes/minikube#requirements), e.g. `kvm2` on
    Linux or `xhyve` on macOS.

1.  [Create a cluster](https://github.com/kubernetes/minikube#quickstart) with
    version 1.9 or greater and your chosen VM driver:

    _Until minikube [enables it by
    default](https://github.com/kubernetes/minikube/pull/2547),the
    MutatingAdmissionWebhook plugin must be manually enabled._

```shell
minikube start \
  --kubernetes-version=v1.9.0 \
  --vm-driver=kvm2 \
  --extra-config=apiserver.Admission.PluginNames=DenyEscalatingExec,LimitRanger,NamespaceExists,NamespaceLifecycle,ResourceQuota,ServiceAccount,DefaultStorageClass,SecurityContextDeny,MutatingAdmissionWebhook
```

### Minikube with GCR

You can use Google Container Registry as the registry for a Minikube cluster.

1.  [Set up a GCR repo](setting-up-a-docker-registry.md). Export the environment
    variable `PROJECT_ID` as the name of your project. Also export `GCR_DOMAIN`
    as the domain name of your GCR repo. This will be either `gcr.io` or a
    region-specific variant like `us.gcr.io`.

    ```shell
    export PROJECT_ID=elafros-demo-project
    export GCR_DOMAIN=gcr.io
    ```

    To have Bazel builds push to GCR, set `DOCKER_REPO_OVERRIDE` to the GCR
    repo's url.

    ```shell
    export DOCKER_REPO_OVERRIDE="${GCR_DOMAIN}/${PROJECT_ID}"
    ```

1.  Create a GCP service account:

    ```shell
    gcloud iam service-accounts create minikube-gcr \
      --display-name "Minikube GCR Pull" \
      --project $PROJECT_ID
    ```

1.  Give your service account the `storage.objectViewer` role:

    ```shell
    gcloud projects add-iam-policy-binding $PROJECT_ID \
      --member "serviceAccount:minikube-gcr@${PROJECT_ID}.iam.gserviceaccount.com" \
      --role roles/storage.objectViewer
    ```

1.  Create a key credential file for the service account:

    ```shell
    gcloud iam service-accounts keys create \
      --iam-account "minikube-gcr@${PROJECT_ID}.iam.gserviceaccount.com" \
      minikube-gcr-key.json
    ```

Now you can use the `minikube-gcr-key.json` file to create image pull secrets
and link them to Kubernetes service accounts. _A secret must be created and
linked to a service account in each namespace that will pull images from GCR._

For example, use these steps to allow Minikube to pull Elafros and Build images
from GCR as built by Bazel (`bazel run :everything.create`). _This is only
necessary if you are not using public Elafros and Build images._

1.  Create a Kubernetes secret in the `ela-system` and `build-system` namespace:

    ```shell
    for prefix in ela build; do
      kubectl create secret docker-registry "gcr" \
        --docker-server=$GCR_DOMAIN \
        --docker-username=_json_key \
        --docker-password="$(cat minikube-gcr-key.json)" \
        --docker-email=your.email@here.com \
        -n "${prefix}-system"
    done
    ```

    _The secret must be created in the same namespace as the pod or service
    account._

1.  Add the secret as an imagePullSecret to the `ela-controller` and
    `build-controller` service accounts:

    ```shell
    for prefix in ela build; do
      kubectl patch serviceaccount "${prefix}-controller" \
        -p '{"imagePullSecrets": [{"name": "gcr"}]}' \
        -n "${prefix}-system"
    done
    ```

1.  Add to your .bashrc:
    ```shell
    # When using Minikube, the K8s user is your local user.
    export K8S_USER_OVERRIDE=$USER
    ```

Use the same procedure to add imagePullSecrets to service accounts in any
namespace. Use the `default` service account for pods that do not specify a
service account.

See also the [private-repo sample README](./../sample/private-repos/README.md).
