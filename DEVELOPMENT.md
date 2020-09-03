# Development

This doc explains how to setup a development environment so you can get started
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

Start by creating [a GitHub account](https://github.com/join), then setup
[GitHub access via SSH](https://help.github.com/articles/connecting-to-github-with-ssh/).

### Install requirements

You must install these tools:

1. [`go`](https://golang.org/doc/install): The language `Knative Serving` is
   built in (1.14 or later)
1. [`git`](https://help.github.com/articles/set-up-git/): For source control
1. [`ko`](https://github.com/google/ko): For development.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For
   managing development environments.

If you're working on and changing `.proto` files:

1. [`protoc`](https://github.com/protocolbuffers/protobuf): For compiling
   protocol buffers.
1. [`protoc-gen-gogofaster`](https://github.com/gogo/protobuf/#more-speed-and-more-generated-code):
   For generating efficient golang code out of protocol buffers.

### Create a cluster and a repo

1. [Set up a kubernetes cluster](https://kubernetes.io/docs/setup/)
   - Follow the instructions in the Kubernetes doc.
1. Set up a docker repository for pushing images. You can use any container
   image registry by adjusting the authentication methods and repository paths
   mentioned in the sections below.
   - [Google Container Registry quickstart](https://cloud.google.com/container-registry/docs/pushing-and-pulling)
   - [Docker Hub quickstart](https://docs.docker.com/docker-hub/)
   - If developing locally with Docker or Minikube, you can set
     `KO_DOCKER_REPO=ko.local` (preferred) or use the `-L` flag to `ko` to build
     and push locally (in this case, authentication is not needed).

**Note**: You'll need to be authenticated with your `KO_DOCKER_REPO` before
pushing images. Run `gcloud auth configure-docker` if you are using Google
Container Registry or `docker login` if you are using Docker Hub.

### Setup your environment

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

### Checkout your fork

The Go tools require that you clone the repository to the
`src/knative.dev/serving` directory in your
[`GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).

To check out this repository:

1. Create your own
   [fork of this repo](https://help.github.com/articles/fork-a-repo/)
1. Clone it to your machine:

```shell
mkdir -p ${GOPATH}/src/knative.dev
cd ${GOPATH}/src/knative.dev
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

Once you've [setup your development environment](#prerequisites), stand up
`Knative Serving`. Note that if you already installed Knative to your cluster,
redeploying the new version should work fine, but if you run into trouble, you
can easily [clean your cluster up](#clean-up) and try again.

### Setup cluster admin

Your user must be a cluster admin to perform the setup needed for Knative. This
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

### Deploy Istio

```shell
STABLE_VERSION=$(curl https://raw.githubusercontent.com/knative-sandbox/net-istio/master/third_party/istio-stable)
kubectl apply -f "https://raw.githubusercontent.com/knative-sandbox/net-istio/master/third_party/${STABLE_VERSION}/istio-crds.yaml"
while [[ $(kubectl get crd gateways.networking.istio.io -o jsonpath='{.status.conditions[?(@.type=="Established")].status}') != 'True' ]]; do
  echo "Waiting on Istio CRDs"; sleep 1
done
kubectl apply -f "https://raw.githubusercontent.com/knative-sandbox/net-istio/master/third_party/${STABLE_VERSION}/istio-minimal.yaml"
kubectl apply -f ./third_party/net-istio.yaml
```

Follow the
[instructions](https://www.knative.dev/docs/serving/gke-assigning-static-ip-address/)
if you need to set up static IP for Ingresses in the cluster.

If you want to adopt preinstalled Istio, please check whether the
`cluster-local-gateway` Service is deployed in namespace `istio-system` or not
(you can check by running
`kubectl get service cluster-local-gateway -n istio-system`). If it's not
installed, please install it with following command. You could also adjust
parameters if needed.

```shell
STABLE_VERSION=$(curl https://raw.githubusercontent.com/knative-sandbox/net-istio/master/third_party/istio-stable)
kubectl apply -f "https://raw.githubusercontent.com/knative-sandbox/net-istio/master/third_party/${STABLE_VERSION}/istio-knative-extras.yaml"
```

> If you want to customize the `istio*.yaml` files you can refer to the
> [Istio installation doc](https://github.com/knative/docs/blob/master/docs/install/installing-istio.md)
> for more information.

### Deploy cert-manager

1. Deploy `cert-manager` CRDs

   ```shell
   kubectl apply -f ./third_party/cert-manager-0.12.0/cert-manager-crds.yaml
   while [[ $(kubectl get crd certificates.cert-manager.io -o jsonpath='{.status.conditions[?(@.type=="Established")].status}') != 'True' ]]; do
     echo "Waiting on Cert-Manager CRDs"; sleep 1
   done
   ```

1. Deploy `cert-manager`

   If you want to use the feature of automatically provisioning TLS for Knative
   services, you need to install the full cert-manager.

   ```shell
   kubectl apply -f ./third_party/cert-manager-0.12.0/cert-manager.yaml
   ```

### Deploy Knative Serving

This step includes building Knative Serving, creating and pushing developer
images and deploying them to your Kubernetes cluster. If you're developing
locally (for example, using
[Docker-on-Mac](https://knative.dev/docs/install/knative-with-docker-for-mac/)),
set `KO_DOCKER_REPO=ko.local` to avoid needing to push your images to an
off-machine registry.

Run:

```shell
ko apply --selector knative.dev/crd-install=true -Rf config/core/
while [[ $(kubectl get crd images.caching.internal.knative.dev -o jsonpath='{.status.conditions[?(@.type=="Established")].status}') != 'True' ]]; do
  echo "Waiting on Knative CRDs"; sleep 1
done

ko apply -Rf config/core/
kubectl apply -f ./third_party/net-istio.yaml

# Optional steps

# Run post-install job to setup nice XIP.IO domain name.  This only works
# if your Kubernetes LoadBalancer has an IPv4 address.
ko delete -f config/post-install/default-domain.yaml --ignore-not-found
ko apply -f config/post-install/default-domain.yaml
```

The above step is equivalent to applying the `serving-crds.yaml`,
`serving-core.yaml`, `serving-hpa.yaml` and `serving-nscert.yaml` for released
versions of Knative Serving and additionally applying **net-istio** as the
ingress implementation.

You can see things running with:

```console
kubectl -n knative-serving get pods
NAME                                  READY   STATUS    RESTARTS   AGE
activator-7454cd659f-rrz86            1/1     Running   0          105s
autoscaler-58cbfd4985-fl5h7           1/1     Running   0          105s
autoscaler-hpa-77964b9b8c-9sbgq       1/1     Running   0          105s
controller-847b7cc977-5mvvq           1/1     Running   0          105s
istio-webhook-69bf66f869-cgc4l        1/1     Running   0          105s
networking-istio-dcf7944fb-5m25h      1/1     Running   0          105s
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

### Install logging and monitoring backends

Run:

```shell
kubectl apply -R -f config/monitoring/100-namespace.yaml \
    -f third_party/config/monitoring/logging/elasticsearch \
    -f config/monitoring/logging/elasticsearch \
    -f third_party/config/monitoring/metrics/prometheus \
    -f config/monitoring/metrics/prometheus \
    -f config/monitoring/tracing/zipkin
```

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

You can delete all of the service components with:

```shell
ko delete --ignore-not-found=true \
  -f config/monitoring/100-namespace.yaml \
  -Rf config/core/ \
  -f ./third_party/net-istio.yaml \
  -f "https://raw.githubusercontent.com/knative-sandbox/net-istio/master/third_party/${STABLE_VERSION}/istio-minimal.yaml" \
  -f "https://raw.githubusercontent.com/knative-sandbox/net-istio/master/third_party/${STABLE_VERSION}/istio-crds.yaml" \
  -f ./third_party/cert-manager-0.12.0/cert-manager-crds.yaml \
  -f ./third_party/cert-manager-0.12.0/cert-manager.yaml
```

## Telemetry

To access Telemetry see:

- [Accessing Metrics](https://www.knative.dev/docs/serving/accessing-metrics/)
- [Accessing Logs](https://www.knative.dev/docs/serving/accessing-logs/)
- [Accessing Traces](https://www.knative.dev/docs/serving/accessing-traces/)
