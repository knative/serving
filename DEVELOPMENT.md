# Development

This doc explains how to set up a development environment so you can get started
[contributing](https://www.knative.dev/contributing/) to `Knative Serving`. Also
take a look at:

- [The pull request workflow](https://www.knative.dev/contributing/contributing/#pull-requests)
- [How to add and run tests](./test/README.md)
- [Iterating](#iterating)

## Prerequisites

Follow the instructions below to set up your development environment. Once you
meet these requirements, you can make changes and
[deploy your own version of Knative Serving](#starting-knative-serving)!

Before submitting a PR, see also [CONTRIBUTING.md](./CONTRIBUTING.md).

### Sign up for GitHub

Start by creating [a GitHub account](https://github.com/join), then set up
[GitHub access via SSH](https://help.github.com/articles/connecting-to-github-with-ssh/).

### Install requirements

You must install these tools:

1. [`go`](https://golang.org/doc/install): The language `Knative Serving` is
   built-in (1.14 or later)
1. [`git`](https://help.github.com/articles/set-up-git/): For source control
1. [`ko`](https://github.com/google/ko): For development.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For
   managing development environments.
1. [`bash`](https://www.gnu.org/software/bash/) v4 or later. On macOS the
   default bash is too old, you can use [Homebrew](https://brew.sh) to install a
   later version.

If you're working on and changing `.proto` files:

1. [`protoc`](https://github.com/protocolbuffers/protobuf): For compiling
   protocol buffers.
1. [`protoc-gen-gogofaster`](https://github.com/gogo/protobuf/#more-speed-and-more-generated-code):
   For generating efficient golang code out of protocol buffers.

### Create a cluster and a repo

1. [Set up a Kubernetes cluster](https://kubernetes.io/docs/setup/)
   - Minimum supported version is 1.18.0
   - Follow the instructions in the Kubernetes doc.
1. Set up a docker repository for pushing images. You can use any container
   image registry by adjusting the authentication methods and repository paths
   mentioned in the sections below.
   - [Google Container Registry quickstart](https://cloud.google.com/container-registry/docs/pushing-and-pulling)
   - [Docker Hub quickstart](https://docs.docker.com/docker-hub/)
   - If developing locally with Docker or Minikube, you can set
     `KO_DOCKER_REPO=ko.local` (preferred) or use the `-L` flag to `ko` to build
     and push locally (in this case, authentication is not needed). If
     developing with kind you can set `KO_DOCKER_REPO=kind.local`.

**Note**: You'll need to be authenticated with your `KO_DOCKER_REPO` before
pushing images. Run `gcloud auth configure-docker` if you are using Google
Container Registry or `docker login` if you are using Docker Hub.

### Set up your environment

To start your environment you'll need to set these environment variables (we
recommend adding them to your `.bashrc`):

1. `GOPATH`: If you don't have one, simply pick a directory and add
   `export GOPATH=...`
1. `$GOPATH/bin` on `PATH`: This is so that tooling installed via `go get` will
   work properly.
1. `KO_DOCKER_REPO`: The docker repository to which developer images should be
   pushed (e.g. `gcr.io/[gcloud-project]`).

- **Note**: if you are using docker hub to store your images your
  `KO_DOCKER_REPO` variable should be `docker.io/<username>`.
- **Note**: Currently Docker Hub doesn't let you create subdirs under your
  username.

`.bashrc` example:

```shell
export GOPATH="$HOME/go"
export PATH="${PATH}:${GOPATH}/bin"
export KO_DOCKER_REPO='gcr.io/my-gcloud-project-id'
```

### Check out your fork

To check out this repository:

1. Create your own
   [fork of this repo](https://help.github.com/articles/fork-a-repo/)
1. Clone it to your machine:

```shell
git clone git@github.com:${YOUR_GITHUB_USERNAME}/serving.git
cd serving
git remote add upstream https://github.com/knative/serving.git
git remote set-url --push upstream no_push
```

_Adding the `upstream` remote sets you up nicely for regularly
[syncing your fork](https://help.github.com/articles/syncing-a-fork/)._

Once you reach this point you are ready to do a full build and deploy as
described below.

## Starting Knative Serving

Once you've [set up your development environment](#prerequisites), stand up
`Knative Serving`. Note that if you already installed Knative to your cluster,
redeploying the new version should work fine, but if you run into trouble, you
can easily [clean your cluster up](#clean-up) and try again.

Enter the `serving` directory to install the following components.

### Set up cluster-admin

Your user must be a cluster-admin to perform the setup needed for Knative. This
should be the case by default if you've provisioned your own Kubernetes cluster.
In particular, you'll need to be able to create Kubernetes cluster-scoped
Namespace, CustomResourceDefinition, ClusterRole, and ClusterRoleBinding
objects.

### Resource allocation for Kubernetes

Please allocate sufficient resources for Kubernetes, especially when you run a
Kubernetes cluster on your local machine. We recommend allocating at least 6
CPUs and 8G memory assuming a single node Kubernetes installation, and
allocating at least 4 CPUs and 8G memory for each node assuming a 3-node
Kubernetes installation. Please go back to
[your cluster setup](https://www.knative.dev/docs/install/) to reconfigure your
Kubernetes cluster in your designated environment, if necessary.

### Deploy cert-manager

1. Deploy `cert-manager`

   ```shell
   kubectl apply -f ./third_party/cert-manager-latest/cert-manager.yaml
   kubectl wait --for=condition=Established --all crd
   kubectl wait --for=condition=Available -n cert-manager --all deployments
   ```

### Deploy Knative Serving

This step includes building Knative Serving, creating and pushing developer
images, and deploying them to your Kubernetes cluster. If you're developing
locally (for example, using
[Docker-on-Mac](https://knative.dev/docs/install/knative-with-docker-for-mac/)),
set `KO_DOCKER_REPO=ko.local` (or `KO_DOCKER_REPO=kind.local` respectively) to
avoid needing to push your images to an off-machine registry.

Run:

```shell
ko apply --selector knative.dev/crd-install=true -Rf config/core/
kubectl wait --for=condition=Established --all crd

ko apply -Rf config/core/

# Optional steps

# Install DomainMapping Components. Enables the DomainMapping feature.
# Needed to pass conformance tests if the `--enable-alpha` flag is passed.
ko apply -Rf config/domain-mapping/

# Run post-install job to set up nice XIP.IO domain name.  This only works
# if your Kubernetes LoadBalancer has an IPv4 address.
ko delete -f config/post-install/default-domain.yaml --ignore-not-found
ko apply -f config/post-install/default-domain.yaml
```

The above step is equivalent to applying the `serving-crds.yaml`,
`serving-core.yaml`, `serving-hpa.yaml` and `serving-nscert.yaml` for released
versions of Knative Serving.

You can see things running with:

```console
kubectl -n knative-serving get pods
NAME                                  READY   STATUS    RESTARTS   AGE
activator-7454cd659f-rrz86            1/1     Running   0          105s
autoscaler-58cbfd4985-fl5h7           1/1     Running   0          105s
autoscaler-hpa-77964b9b8c-9sbgq       1/1     Running   0          105s
controller-847b7cc977-5mvvq           1/1     Running   0          105s
networking-ns-cert-56c58544db-sgstd   1/1     Running   0          105s
webhook-6b6c77567f-flr59              1/1     Running   0          105s
```

You can access the Knative Serving Controller's logs with:

```shell
kubectl -n knative-serving logs $(kubectl -n knative-serving get pods -l app=controller -o name) -c controller
```

If you're using a GCP project to host your Kubernetes cluster, it's good to
check the
[Discovery & load balancing](http://console.developers.google.com/kubernetes/discovery)
page to ensure that all services are up and running (and not blocked by a quota
issue, for example).

### Deploy Knative Ingress

Knative supports a variety of Ingress solutions.

For simplicity, you can just run the following command to install Kourier.

```
kubectl apply -f ./third_party/kourier-latest/kourier.yaml

kubectl patch configmap/config-network \
  -n knative-serving \
  --type merge \
  -p '{"data":{"ingress.class":"kourier.ingress.networking.knative.dev"}}'
```

If you want to choose another Ingress solution, you can follow step 3 in the
[Knative installation doc](https://knative.dev/docs/install/any-kubernetes-cluster/#installing-the-serving-component)
to pick up an alternative Ingress solution and install it.

## Iterating

As you make changes to the code-base, there are two special cases to be aware
of:

- **If you change an input to generated code**, then you must run
  [`./hack/update-codegen.sh`](./hack/update-codegen.sh). Inputs include:

  - API type definitions in [pkg/apis/serving/v1/](./pkg/apis/serving/v1/.).
  - Type definitions annotated with `// +k8s:deepcopy-gen=true`.
  - The `_example` value of config maps (to keep the
    `knative.dev/example-checksum` annotations in sync). These can also be
    individually updated using `./hack/update-checksums.sh`.
  - `.proto` files. Run `./hack/update-codegen.sh` with the
    `--generate-protobufs` flag to enable protocol buffer generation.

- **If you change a package's deps** (including adding an external dependency),
  then you must run [`./hack/update-deps.sh`](./hack/update-deps.sh).

These are both idempotent, and we expect that running these at `HEAD` to have no
diffs. Code generation and dependencies are automatically checked to produce no
diffs for each pull request.

update-deps.sh runs go get/mod command. In some cases, if newer dependencies are
required, you need to run "go get" manually.

Once the codegen and dependency information is correct, redeploying the
controller is simply:

```shell
ko apply -f config/core/deployments/controller.yaml
```

Or you can [clean it up completely](./DEVELOPMENT.md#clean-up) and
[completely redeploy `Knative Serving`](./DEVELOPMENT.md#starting-knative-serving).

### Updating existing dependencies

To update existing dependencies execute

```shell
./hack/update-deps.sh --upgrade && ./hack/update-codegen.sh
```

## Clean up

You can delete all of the serving components with:

```shell
ko delete --ignore-not-found=true \
  -Rf config/core/ \
  -f ./third_party/kourier-latest/kourier.yaml \
  -f ./third_party/cert-manager-latest/cert-manager.yaml
```
