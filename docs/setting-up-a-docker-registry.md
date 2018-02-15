# Setting Up A Docker Registry

This document explains how to use different Docker registries with Elafros. It
assumes you have gone through the steps listed in
[DEVELOPMENT.md](/DEVELOPMENT.md) to set up your development environment (or
that you at least have installed `go`, set `GOPATH`, and put `$GOPATH/bin` on
your `PATH`).

It currently only contains instructions for [Google Container Registry
(GCR)](https://cloud.google.com/container-registry/), but you should be able to
use any Docker registry.

## Google Container Registry (GCR)

### Required Tools

Install the following tools:

1.  [`gcloud`](https://cloud.google.com/sdk/downloads)
1.  [`docker-credential-gcr`](https://github.com/GoogleCloudPlatform/docker-credential-gcr)

`docker-credential-gcr` can be installed with `gcloud components install
docker-credential-gcr` after you install `gcloud`. Note that some methods of
`gcloud` installation don't allow you to install components, in which case you
can build from source:

```shell
go get github.com/GoogleCloudPlatform/docker-credential-gcr
cd "$GOPATH/src/github.com/GoogleCloudPlatform/docker-credential-gcr"
make && mv ./bin/docker-credential-gcr "${GOPATH}/bin/"
```

### Setup

```shell
# Authenticate yourself with the gcloud CLI:
gcloud auth login

# Choose a name for your project.
export PROJECT_ID=YOUR_PROJECT_ID_HERE_YOU_MUST_PICK_A_NEW_ONE
gcloud projects create "${PROJECT_ID}"

# Enable the GCR API.
gcloud --project="${PROJECT_ID}" service-management enable \
    containerregistry.googleapis.com

# Hook up your GCR credentials. Note that this may complain if you don't have
# the docker CLI installed, but it is not necessary and should still work.
docker-credential-gcr configure-docker
```

If you need to, update your `DOCKER_REPO_OVERRIDE` in your `.bashrc`. It should
now be `export DOCKER_REPO_OVERRIDE='us.gcr.io/<your-project-id>`. (Or maybe a
different region instead of 'us' if you didn't pick a 'us' Google Cloud region.)

You may need to run `bazel clean` after updating your `DOCKER_REPO_OVERRIDE`
variable for `bazel` to pick up the change.

That's it, you're done!
