# Elafros environments

There are two ready-to-use stable Elafros environments available for contributors.

Currently the access is restricted to members of the `elafros-images` Google groups. Ask @mattmoor to be added to the group.

## The demo environment

This environment is rebuilt by the Steering Committee at the conclusion of a milestone.

You can configure your access by running:

```
gcloud container clusters get-credentials elafros-demo --zone us-central1-a --project elafros-environments
```

### The playground environment

This environment is recreated by a prow periodic job every Saturday 1AM PST, using the latest stable Elafros release (i.e., the images available at gcr.io/elafros-images).

You can configure your access by running:

```
gcloud container clusters get-credentials elafros-playground --zone us-central1-a --project elafros-environments
```

### Manually recreating the environments

To manually recreate an environment, call the `deploy.sh` script passing the environment name as parameter:

```
./deploy.sh elafros-playground # or
./deploy.sh elafros-demo
```

The script will create the Kubernetes cluster (shutting down any existing instance) and install the latest stable Elafros release.
