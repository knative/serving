# Development

This doc explains how to setup a development environment so you can get started
[contributing](https://github.com/knative/docs/blob/master/community/CONTRIBUTING.md)
to `Knative Serving`. Also take a look at:

- [The pull request workflow](https://github.com/knative/docs/blob/master/community/CONTRIBUTING.md#pull-requests)
- [How to add and run tests](./test/README.md)
- [Iterating](#iterating)

## Prerequisites <a name="getting-started"></a>

Follow the instructions below to set up your development environment. Once you
meet these requirements, you can make changes and
[deploy your own version of Knative Serving](#starting-knative-serving)!

Before submitting a PR, see also [CONTRIBUTING.md](./CONTRIBUTING.md).

### Sign up for GitHub

Start by creating [a GitHub account](https://github.com/join), then setup
[GitHub access via SSH](https://help.github.com/articles/connecting-to-github-with-ssh/).

### Install requirements <a name="requirements"></a>

You must install these tools:

1. [`go`](https://golang.org/doc/install): The language `Knative Serving` is
   built in
1. [`git`](https://help.github.com/articles/set-up-git/): For source control
1. [`dep`](https://github.com/golang/dep): For managing external Go
   dependencies.
1. [`ko`](https://github.com/google/go-containerregistry/tree/master/cmd/ko):
   For development.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For
   managing development environments.

### Create a cluster and a repo

1. [Set up a kubernetes cluster](./docs/creating-a-kubernetes-cluster.md). You
   do _not_ need install Istio or Knative using the instructions in that page -
   simply create the cluster and come back here. However, if you _did_ install
   Istio/Knative following those instructions, that's fine too, you'll just
   redeploy over them, below.
1. [Set up a docker repository you can push to](./docs/setting-up-a-docker-registry.md)

### Setup your environment <a name="environment-setup"></a>

To start your environment you'll need to set these environment variables (we
recommend adding them to your `.bashrc`):

1. `GOPATH`: If you don't have one, simply pick a directory and add
   `export GOPATH=...`
1. `$GOPATH/bin` on `PATH`: This is so that tooling installed via `go get` will
   work properly.
1. `KO_DOCKER_REPO` and `DOCKER_REPO_OVERRIDE`: The docker repository to which
   developer images should be pushed (e.g. `gcr.io/[gcloud-project]`).
   - **Note**: if you are using docker hub to store your images your
     `KO_DOCKER_REPO` variable should be `docker.io/<username>`.
   - **Note**: Currently Docker Hub doesn't let you create subdirs under your
     username.

`.bashrc` example:

```shell
export GOPATH="$HOME/go"
export PATH="${PATH}:${GOPATH}/bin"
export KO_DOCKER_REPO='gcr.io/my-gcloud-project-name'
export DOCKER_REPO_OVERRIDE="${KO_DOCKER_REPO}"
export K8S_CLUSTER_OVERRIDE='my-k8s-cluster-name'
```

Make sure to configure
[authentication](https://cloud.google.com/container-registry/docs/advanced-authentication#standalone_docker_credential_helper)
for your `KO_DOCKER_REPO` if required. To be able to push images to
`gcr.io/<project>`, you need to run this once:

```shell
gcloud auth configure-docker
```

For `K8S_CLUSTER_OVERRIDE`, we expect that this name matches a cluster with
authentication configured with `kubectl`. You can list the clusters you
currently have configured via: `kubectl config get-contexts`. For the cluster
you want to target, the value in the CLUSTER column should be put in this
variable.

### Checkout your fork

The Go tools require that you clone the repository to the
`src/github.com/knative/serving` directory in your
[`GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).

To check out this repository:

1. Create your own
   [fork of this repo](https://help.github.com/articles/fork-a-repo/)
1. Clone it to your machine:

```shell
mkdir -p ${GOPATH}/src/github.com/knative
cd ${GOPATH}/src/github.com/knative
git clone git@github.com:${YOUR_GITHUB_USERNAME}/serving.git
cd serving
git remote add upstream git@github.com:knative/serving.git
git remote set-url --push upstream no_push
```

_Adding the `upstream` remote sets you up nicely for regularly
[syncing your fork](https://help.github.com/articles/syncing-a-fork/)._

Once you reach this point you are ready to do a full build and deploy as
described below.

## Starting Knative Serving

Once you've [setup your development environment](#getting-started), stand up
`Knative Serving`. Note that if you already installed Knative to your cluster,
redeploying the new version should work fine, but if you run into trouble, you
can easily [clean your cluster up](#clean-up) and try again.

### Setup cluster admin

Your `$K8S_USER_OVERRIDE` must be a cluster admin to perform the setup needed
for Knative.

The value you use depends on
[your cluster setup](./docs/creating-a-kubernetes-cluster.md):

```shell
# When using Minikube, the K8s user is your local user.
export K8S_USER_OVERRIDE=$USER

# When using GKE, the K8s user is your GCP user.
export K8S_USER_OVERRIDE=$(gcloud config get-value core/account)

kubectl create clusterrolebinding cluster-admin-binding \
  --clusterrole=cluster-admin \
  --user="${K8S_USER_OVERRIDE?}"
```

### Deploy Istio

```shell
kubectl apply -f ./third_party/istio-1.0.2/istio-crds.yaml
while [ $(kubectl get crd gateways.networking.istio.io -o jsonpath='{.status.conditions[?(@.type=="Established")].status}') != 'True' ]; do
  echo "Waiting on Istio CRDs"; sleep 1
done
kubectl apply -f ./third_party/istio-1.0.2/istio.yaml
```

Follow the [instructions](./docs/setting-up-ingress-static-ip.md) if you need to
set up static IP for Ingresses in the cluster.

### Deploy Knative Serving

This step includes building Knative Serving, creating and pushing developer
images and deploying them to your Kubernetes cluster.

First, edit [config-network.yaml](config/config-network.yaml) as instructed
within the file. If this file is edited and deployed after Knative Serving
installation, the changes in it will be effective only for newly created
revisions.

Next, run:

```shell
ko apply -f config/
```

You can see things running with:

```shell
kubectl -n knative-serving get pods
NAME                                READY     STATUS    RESTARTS   AGE
controller-77897cc687-vp27q   1/1       Running   0          16s
webhook-5cb5cfc667-k7mcg      1/1       Running   0          16s
```

You can access the Knative Serving Controller's logs with:

```shell
kubectl -n knative-serving logs $(kubectl -n knative-serving get pods -l app=controller -o name)
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

  - API type definitions in
    [pkg/apis/serving/v1alpha1/](./pkg/apis/serving/v1alpha1/.),
  - Types definitions annotated with `// +k8s:deepcopy-gen=true`.

- **If you change a package's deps** (including adding external dep), then you
  must run [`./hack/update-deps.sh`](./hack/update-deps.sh).

These are both idempotent, and we expect that running these at `HEAD` to have no
diffs. Code generation and dependencies are automatically checked to produce no
diffs for each pull request.

update-deps.sh runs "dep ensure" command. In some cases, if newer dependencies
are required, you need to run "dep ensure -update package-name" manually.

Once the codegen and dependency information is correct, redeploying the
controller is simply:

```shell
ko apply -f config/controller.yaml
```

Or you can [clean it up completely](./DEVELOPMENT.md#clean-up) and
[completely redeploy `Knative Serving`](./DEVELOPMENT.md#starting-knative-serving).

## Clean up

You can delete all of the service components with:

```shell
ko delete --ignore-not-found=true \
  -f config/monitoring/100-namespace.yaml \
  -f config/ \
  -f ./third_party/config/build/release.yaml \
  -f ./third_party/istio-1.0.2/istio.yaml \
  -f ./third_party/istio-1.0.2/istio-crds.yaml
```

## Telemetry

See [telemetry documentation](./docs/telemetry.md).
