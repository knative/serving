# Knative Serving environments

There are two ready-to-use stable Knative Serving environments available for contributors.

Currently the access is restricted to members of the [knative-dev@](https://groups.google.com/forum/#!forum/knative-dev) Google group.

## The demo environment

This environment is rebuilt by the Steering Committee at the conclusion of a milestone.

You can configure your access by running:

```shell
gcloud container clusters get-credentials knative-demo --zone us-central1-a --project knative-environments
```

### The playground environment

This environment is recreated by a prow periodic job every Saturday 1AM PST, using the latest stable Knative Serving release (i.e., the images available at gcr.io/knative-images).

You can configure your access by running:

```shell
gcloud container clusters get-credentials knative-playground --zone us-central1-a --project knative-environments
```

### Manually recreating the environments

To manually recreate an environment, call the `deploy.sh` script passing the environment name as parameter:

```shell
./deploy.sh knative-playground # or
./deploy.sh knative-demo
```

The script will create the Kubernetes cluster (shutting down any existing instance) and install the latest stable Knative Serving release.
