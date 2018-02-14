# Creating a Kubernetes Cluster for Elafros

Two options:

* Setup a [GKE cluster](#gke)
* Run [minikube](#minikube) locally

## GKE

To use a k8s cluster running in GKE:

1.  Install `gcloud` using
   [the instructions for your platform](https://cloud.google.com/sdk/downloads).

2.  Create a GCP project (or use an existing project if you've already created
    one) at http://console.cloud.google.com/home/dashboard. Set the ID of the
    project in an environment variable (e.g. `PROJECT_ID`) along with the email
    of your GCP user (`GCP_USER`).

3.  Enable the k8s API:

    ```shell
    gcloud --project=$PROJECT_ID services enable container.googleapis.com
    ```

4.  Create a k8s cluster:

    ```shell
    gcloud --project=$PROJECT_ID container clusters create \
      --cluster-version=1.9.2-gke.1 \
      --zone=us-east1-d \
      --scopes=cloud-platform \
      --enable-autoscaling --min-nodes=1 --max-nodes=3 \
      elafros-demo
    ```
    - Version 1.9+ is required
    - Change this to whichever zone you choose
    - cloud-platform scope is required to access GCB
    - Autoscale from 1 to 3 nodes. Adjust this for your use case
    - Change this to your preferred cluster name


    You can see the list of supported cluster versions in a particular zone
    by running:

    ```shell
    # Get the list of valid versions in us-east1-d
    gcloud container get-server-config --zone us-east1-d
    ```

5. If you haven't installed `kubectl` yet, you can install it now with `gcloud`:

    ```shell
    gcloud components install kubectl
    ```

6.  Give your gcloud user cluster-admin privileges:

    ```shell
    kubectl create clusterrolebinding gcloud-admin-binding \
        --clusterrole=cluster-admin \
        --user=$GCP_USER
    ```

7. Enable the GCR API:

   ```shell
   gcloud --project=$PROJECT_ID service-management enable containerregistry.googleapis.com
   ```

8. Install the `docker-credential-gcr` helper so Docker (and Bazel) can
   authenticate with GCR:

   ```shell
   gcloud components install docker-credential-gcr
   ```

9. Add the GCR credentials to the Docker config file:

   ```shell
   docker-credential-gcr configure-docker
   ```

Now you can use `gcr.io/$PROJECT_ID` as your Docker repo and your GKE
cluster will automatically pull from it.

## Minikube

To run a k8s cluster locally, you will need to [install and configure
minikube](https://github.com/kubernetes/minikube#minikube) with a [VM
driver](https://github.com/kubernetes/minikube#requirements), e.g. `kvm` on
Linux or `xhyve` on macOS.

If this worked, you should be able to walk through [the minikube
quickstart](https://github.com/kubernetes/minikube#quickstart) successfully.

```shell
minikube start \
# Kubernetes version must be at least 1.9.0
--kubernetes-version=v1.9.0 \
# Use the VM driver you installed above
--vm-driver=kvm
```

_TODO Add instructions for setting up a local registry_
