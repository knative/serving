# Development

This doc explains how to setup a development environment so you can get started
[contributing](./community/CONTRIBUTING.md) to `Knative Serving`. Also take a look at:

* [The pull request workflow](./community/CONTRIBUTING.md#pull-requests)
* [How to add and run tests](./test/README.md)
* [Iterating](#iterating)

## Getting started

1. Create [a GitHub account](https://github.com/join)
1. Setup [GitHub access via
   SSH](https://help.github.com/articles/connecting-to-github-with-ssh/)
1. Install [requirements](#requirements)
1. [Set up a kubernetes cluster](./docs/creating-a-kubernetes-cluster.md)
1. [Set up a docker repository you can push
   to](./docs/setting-up-a-docker-registry.md)
1. Set up your [shell environment](#environment-setup)
1. [Create and checkout a repo fork](#checkout-your-fork)

Once you meet these requirements, you can [start Knative Serving](#starting-knative-serving)!

Before submitting a PR, see also [CONTRIBUTING.md](./CONTRIBUTING.md).

### Requirements

You must install these tools:

1. [`go`](https://golang.org/doc/install): The language `Knative Serving` is built in
1. [`git`](https://help.github.com/articles/set-up-git/): For source control
1. [`dep`](https://github.com/golang/dep): For managing external Go
   dependencies.
1. [`ko`](https://github.com/google/go-containerregistry/tree/master/cmd/ko): For
development.
1. or [`bazel`](https://docs.bazel.build/versions/master/getting-started.html): For
   development.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For
   managing development environments.

### Environment setup

To [start your environment](./README.md#start-knative) you'll need to set these environment
variables (we recommend adding them to your `.bashrc`):

1. `GOPATH`: If you don't have one, simply pick a directory and add
`export GOPATH=...`
1. `$GOPATH/bin` on `PATH`: This is so that tooling installed via `go get` will
work properly.
1. `KO_DOCKER_REPO` and `DOCKER_REPO_OVERRIDE`: The docker repository to which
developer images should be pushed (e.g. `gcr.io/[gcloud-project]`).
1. `K8S_CLUSTER_OVERRIDE`: The Kubernetes cluster on which development
environments should be managed.
1. `K8S_USER_OVERRIDE`: The Kubernetes user that you use to manage your cluster.
This depends on your cluster setup, please take a look at [cluster setup
instruction](./docs/creating-a-kubernetes-cluster.md).

`.bashrc` example:

```shell
export GOPATH="$HOME/go"
export PATH="${PATH}:${GOPATH}/bin"
export KO_DOCKER_REPO='gcr.io/my-gcloud-project-name'
export DOCKER_REPO_OVERRIDE="${KO_DOCKER_REPO}"
export K8S_CLUSTER_OVERRIDE='my-k8s-cluster-name'
export K8S_USER_OVERRIDE='my-k8s-user'
```

(Make sure to configure [authentication](
https://cloud.google.com/container-registry/docs/advanced-authentication#standalone_docker_credential_helper)
for your `KO_DOCKER_REPO` if required.)

For `K8S_CLUSTER_OVERRIDE`, we expect that this name matches a cluster with authentication configured
with `kubectl`.  You can list the clusters you currently have configured via:
`kubectl config get-contexts`.  For the cluster you want to target, the value in the CLUSTER column
should be put in this variable.

_It is notable that if you change the `*_OVERRIDE` variables, you may need to
`bazel clean` in order to properly pick up the change (if using bazel)._

### Checkout your fork

The Go tools require that you clone the repository to the `src/github.com/knative/serving` directory
in your [`GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).

To check out this repository:

1. Create your own [fork of this
  repo](https://help.github.com/articles/fork-a-repo/)
2. Clone it to your machine:
  ```shell
  mkdir -p ${GOPATH}/src/github.com/knative
  cd ${GOPATH}/src/github.com/knative
  git clone git@github.com:${YOUR_GITHUB_USERNAME}/serving.git
  cd serving
  git remote add upstream git@github.com:knative/serving.git
  git remote set-url --push upstream no_push
  ```

_Adding the `upstream` remote sets you up nicely for regularly [syncing your
fork](https://help.github.com/articles/syncing-a-fork/)._

## Starting Knative Serving

Once you've [setup your development environment](#getting-started), stand up `Knative Serving` with:

### Deploy Istio

```shell
kubectl create clusterrolebinding cluster-admin-binding \
  --clusterrole=cluster-admin \
  --user="${K8S_USER_OVERRIDE}"

kubectl apply -f ./third_party/istio-0.8.0/istio.yaml
```

Then label namespaces with `istio-injection=enabled`:

```shell
kubectl label namespace default istio-injection=enabled
```

### Deploy Build

```shell
kubectl apply -f ./third_party/config/build/release.yaml
```

### Deploy Knative Serving

```shell
# With ko
ko apply -f config/

# With bazel
bazel run //config:everything.apply
```

You can see things running with:
```shell
kubectl -n knative-serving-system get pods
NAME                                READY     STATUS    RESTARTS   AGE
ela-controller-77897cc687-vp27q   1/1       Running   0          16s
ela-webhook-5cb5cfc667-k7mcg      1/1       Running   0          16s
```

You can access the Knative Serving Controller's logs with:

```shell
kubectl -n knative-serving-system logs $(kubectl -n knative-serving-system get pods -l app=ela-controller -o name)
```

If you're using a GCP project to host your Kubernetes cluster, it's good to check the
[Discovery & load balancing](http://console.developers.google.com/kubernetes/discovery)
page to ensure that all services are up and running (and not blocked by a quota issue, for example).

### Enable log and metric collection
You can use two different setups for collecting logs and metrics:
1. **everything**: This configuration collects logs & metrics from user containers, build controller and istio requests.

```shell
# With kubectl
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-prod \
    -f third_party/config/monitoring \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml

# With bazel
bazel run config/monitoring:everything.apply
```

2. **everything-dev**: This configuration collects everything in (1) plus Knative Serving controller logs.

```shell
# With kubectl
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-dev \
    -f third_party/config/monitoring \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml

# With bazel
bazel run config/monitoring:everything-dev.apply
```

Once complete, follow the instructions at [Logs and Metrics](./docs/telemetry.md)

## Iterating

As you make changes to the code-base, there are two special cases to be aware of:
* **If you change a type definition ([pkg/apis/serving/v1alpha1/](./pkg/apis/serving/v1alpha1/.)),** then you must run [`./hack/update-codegen.sh`](./hack/update-codegen.sh).
* **If you change a package's deps** (including adding external dep), then you must run
  [`./hack/update-deps.sh`](./hack/update-deps.sh).

These are both idempotent, and we expect that running these at `HEAD` to have no diffs.

Once the codegen and dependency information is correct, redeploying the controller is simply:
```shell
# With ko
ko apply -f config/controller.yaml

# With bazel
bazel run //config:controller.apply
```

Or you can [clean it up completely](./README.md#clean-up) and [completely
redeploy `Knative Serving`](./README.md#start-knative).

## Clean up

You can delete all of the service components with:
```shell
# With ko
ko delete --ignore-not-found=true \
  -f config/ \
  -f ./third_party/config/build/release.yaml \
  -f ./third_party/istio-0.8.0/istio.yaml

# With bazel
bazel run //config:everything.delete
```

## Telemetry

See [telemetry documentation](./docs/telemetry.md).
