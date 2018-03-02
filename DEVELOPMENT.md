# Development

This doc explains how to setup a development environment so you can get started
[contributing](./CONTRIBUTING.md) to `Elafros`. Also take a look at [the 
development workflow](./CONTRIBUTING.md#workflow) and [the test docs](./test/README.md).

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

Once you meet these requirements, you can [start an `Elafros`
environment](README.md#start-elafros)!

Before submitting a PR, see also [CONTRIBUTING.md](./CONTRIBUTING.md).

### Requirements

You must install these tools:

1. [`go`](https://golang.org/doc/install): The language `Elafros` is built in
1. [`git`](https://help.github.com/articles/set-up-git/): For source control
1. [`dep`](https://github.com/golang/dep): For managing external Go
   dependencies.
1. [`bazel`](https://docs.bazel.build/versions/master/getting-started.html): For
   performing builds.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For
   managing development environments.

### Environment setup

To [start your environment](./README.md#start-elafros) you'll need to set these environment
variables (we recommend adding them to your `.bashrc`):

1. `GOPATH`: If you don't have one, simply pick a directory and add `export GOPATH=...`
1. `$GOPATH/bin` on `PATH`: This is so that tooling installed via `go get` will work properly.
1. `DOCKER_REPO_OVERRIDE`: The docker repository to which developer images should be pushed (e.g. `gcr.io/[gcloud-project]`).
1. `K8S_CLUSTER_OVERRIDE`: The Kubernetes cluster on which development environments should be managed.

`.bashrc` example:

```shell
export GOPATH='~/go'
export PATH="${PATH}:${GOPATH}/bin"
export DOCKER_REPO_OVERRIDE='gcr.io/my-gcloud-project-name'
export K8S_CLUSTER_OVERRIDE='my-k8s-cluster-name'
```

(Make sure to configure [authentication](https://github.com/bazelbuild/rules_docker#authentication) for your
`DOCKER_REPO_OVERRIDE` if required.)

For `K8S_CLUSTER_OVERRIDE`, we expect that this name matches a cluster with authentication configured
with `kubectl`.  You can list the clusters you currently have configured via:
`kubectl config get-contexts`.  For the cluster you want to target, the value in the CLUSTER column
should be put in this variable.

These environment variables will be provided to `bazel` via
[`print-workspace-status.sh`](print-workspace-status.sh) to
[stamp](https://github.com/bazelbuild/rules_docker#stamping) the variables in
[`WORKSPACE`](WORKSPACE).

_It is notable that if you change the `*_OVERRIDE` variables, you may need to
`bazel clean` in order to properly pick up the change._

### Checkout your fork

The Go tools require that you clone the repository to the `src/github.com/elafros/elafros` directory
in your [`GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).

To check out this repository:

1. Create your own [fork of this
  repo](https://help.github.com/articles/fork-a-repo/)
2. Clone it to your machine:
  ```shell
  mkdir -p ${GOPATH}/src/github.com/elafros
  cd ${GOPATH}/src/github.com/elafros
  git clone git@github.com:${YOUR_GITHUB_USERNAME}/elafros.git
  cd elafros
  git remote add upstream git@github.com:elafros/elafros.git
  git remote set-url --push upstream no_push
  ```

_Adding the `upstream` remote sets you up nicely for regularly [syncing your
fork](https://help.github.com/articles/syncing-a-fork/)._

Once you reach this point you are ready to do a full build and deploy as described [here](./README.md#start-elafros).

## Iterating

As you make changes to the code-base, there are two special cases to be aware of:
* **If you change a type definition ([pkg/apis/ela/v1alpha1/](./pkg/apis/ela/v1alpha1/.)),** then you must run [`./hack/update-codegen.sh`](./hack/update-codegen.sh).
* **If you change a package's deps** (including adding external dep), then you must run
  [`./hack/update-deps.sh`](./hack/update-deps.sh).

These are both idempotent, and we expect that running these at `HEAD` to have no diffs.

Once the codegen and dependency information is correct, redeploying the controller is simply:
```shell
bazel run :controller.apply
```

Or you can [clean it up completely](./README.md#clean-up) and [completely
redeploy `Elafros`](./README.md#start-elafros).

## Tests

Running tests as you make changes to the code-base is pretty simple. See [the test docs](./test/README.md).

## Telemetry

See [telemetry documentation](./docs/telemetry.md).
