# Development

## Getting started

* Create [a github account](https://github.com/join)
* Install [requirements](#requirements)
* Setup [environment](#environment-setup)

Before submitting a PR, see also [CONTRIBUTING.md](./CONTRIBUTING.md).

### Requirements

You must install these tools:

1. [`go`](https://golang.org/doc/install): The language `Elafros` is built in
1. [`git`](https://help.github.com/articles/set-up-git/): For source control
1. [`dep`](https://github.com/golang/dep): For managing external Go dependencies.
1. [`bazel`](https://docs.bazel.build/versions/master/getting-started.html): For performing builds.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For managing development environments.


You'll also need to setup:

1. [This repo](#setup-your-repo)
1. A running kubernetes cluster (for example, using
   [minikube](https://github.com/kubernetes/minikube)); your user **must** have
   cluster-admin privileges:
   ```bash
   kubectl create clusterrolebinding cluster-admin-binding
   --clusterrole=cluster-admin  --user=${YOUR_KUBE_USER}
   ```
1. A docker repository you can push to
1. [Your environment](#environment-setup)

Once you meet these requirements, you can [start an `Elafros`
environment](README.md#start-elafros)!

### Setup your repo

The repository must be set up properly relative to your
[`GOPATH`](https://github.com/golang/go/wiki/SettingGOPATH).

To check out this repository first [fork this
repo](https://help.github.com/articles/fork-a-repo/), then:

```shell
mkdir -p ${GOPATH}/src/github.com/google/elafros
git clone git@github.com:${YOUR_GITHUB_USERNAME}/elafros.git
git remote add upstream git@github.com:google/elafros.git
git remote set-url --push upstream no_push
```
### Environment setup

To [start your envrionment](./README.md#start-elafros) you'll need to set these environment
variables (we recommend adding them to your `.bashrc`):

1. `GOPATH`: If you don't have one, simply pick a directory and add `export GOPATH=...`
1. `$GOPATH/bin` on `PATH`: This is so that tooling installed via `go get` will work properly.
1. `DOCKER_REPO_OVERRIDE`: The docker repository to which developer images should be pushed.
1. `K8S_CLUSTER_OVERRIDE`: The Kubernetes cluster on which development environments should be managed.

(Make sure to configure [authentication](https://github.com/bazelbuild/rules_docker#authorization) for your
`DOCKER_REPO_OVERRIDE` if required.)

For `K8S_CLUSTER_OVERRIDE`, we expect that this name matches a cluster with authentication configured
with `kubectl`.  You can list the clusters you currently have configured via:
`kubectl config get-contexts`.  For the cluster you want to target, the value in the cluster column
should be put in this variable.

These environment variables will be provided to `bazel` via
[`print-workspace-status.sh`](print-workspace-status.sh) to
[stamp](https://github.com/bazelbuild/rules_docker#stamping) the variables in
[`WORKSPACE`](WORKSPACE).

_It is notable that if you change the `*_OVERRIDE` variables, you may need to `bazel clean` in order
to properly pick up the change._

### Iterating

As you make changes to the code-base, there are two special cases to be aware of:
* **If you change a type definition ([pkg/apis/ela/v1alpha1/](./pkg/apis/ela/v1alpha1/.)),** then you must run [`./hack/update-codegen.sh`](./hack/update-codegen.sh).
* **If you change a package's deps** (including adding external dep), then you must run
  [`./hack/update-deps.sh`](./hack/update-deps.sh).

These are both idempotent, and we expect that running these at `HEAD` to have no diffs.

Once the codegen and dependency information is correct, redeploying the controller is simply:
```shell
bazel run :controller.replace
```

Or you can [clean it up completely](./README.md#clean-up) and [completely
redeploy `Elafros`](./README.md#start-elafros).
