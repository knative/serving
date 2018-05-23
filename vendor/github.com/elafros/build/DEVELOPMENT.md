# Development

This doc explains how to setup a development environment so you can get started
[contributing](./CONTRIBUTING.md).

## Getting Started

1. [Check out the repository](#checking-out-the-repository)
1. [Run the controller (ko)](#running-the-controller-ko)
1. Or [run the controller (bazel)](#running-the-controller-bazel)
1. [Running integration tests](#running-integration-tests)

## Checking out the repository

To set the paths of the imports right, make sure you clone into the directory
`${GOPATH}/src/github.com/elafros/build`.  For example:

```shell
# Set up GOPATH
$ export GOPATH=$(pwd)/go  # Choose your directory.
$ mkdir -p ${GOPATH}

# Grab the repo itself.
$ go get github.com/elafros/build
$ cd ${GOPATH}/src/github.com/elafros/build

# Optionally add your remote.
$ git remote add ${USER} https://github.com/${USER}/build
```

## Running the Controller (ko)

### One-time setup

Configure `ko` to point to your registry:

```shell
# You can put these definitions in .bashrc, so this is one-time setup.
export KO_DOCKER_REPO=us.gcr.io/project

# Install "ko" from our vendor snapshot.
go install ./vendor/github.com/google/go-containerregistry/cmd/ko

# Check that "ko" is on your path
which ko
```

Note that this expects your Docker authorization is [properly configured](
https://cloud.google.com/container-registry/docs/advanced-authentication#standalone_docker_credential_helper).

### Standing it up

You can stand up a version of this controller on-cluster with:
```shell
# This will register the CRD and deploy the controller to start acting on them.
ko apply -f config/
```

**NOTE:** This will deploy to `kubectl config current-context`.

### Iterating

As you make changes to the code, you can redeploy your controller with:
```shell
ko apply -f config/controller.yaml
```

**Two things of note:**
1. If your (external) dependencies have changed, you should:
   `./hack/update-deps.sh`.
1. If your type definitions have changed, you should:
   `./hack/update-codegen.sh`.

### Cleanup

You can clean up everything with:
```shell
ko delete -f config/
```

## Running the Controller (bazel)

### One-time setup

To tell Bazel where to publish images, and to which cluster to deploy:

```shell
# You can put these definitions in .bashrc, so this is one-time setup.
export DOCKER_REPO_OVERRIDE=us.gcr.io/project
# See: kubectl config get-contexts
export K8S_CLUSTER_OVERRIDE=cluster-name

# Forces Bazel to pick up these changes (don't put in .bashrc)
bazel clean
```

Note that this expects your Docker authorization is [properly configured](
https://cloud.google.com/container-registry/docs/advanced-authentication#standalone_docker_credential_helper).

### Standing it up

You can stand up a version of this controller on-cluster with:
```shell
# This will register the CRD and deploy the controller to start acting on them.
bazel run //config:everything.create
```

### Iterating

As you make changes to the code, you can redeploy your controller with:
```shell
bazel run //config:controller.replace
```

**Two things of note:**
1. If your (external) dependencies have changed, you should:
   `./hack/update-deps.sh`.
1. If your type definitions have changed, you should:
   `./hack/update-codegen.sh`.

If only internal dependencies have changed, and you want to avoid the `dep`
portion of `./hack/update-deps.sh`, you can just run `Gazelle` with:
```shell
bazel run //:gazelle -- -proto=disable
```

### Cleanup

You can clean up everything with:
```shell
bazel run //config:everything.delete
```

## Running Integration Tests

To run integration tests, run the following steps:

```shell
# First, have the version of the system that you want to test up.
# e.g. to change between builders, alter the flag in controller.yaml
ko apply -f config/

# Next, make sure that you have no builds or build templates in your current namespace:
kubectl delete builds --all
kubectl delete buildtemplates --all

# Launch the test suite (this can be cleaned up with "ko delete -R -f tests/")
ko apply -R -f tests/
```

You can track the progress of your builds with this command, which will also
format the output nicely.

```shell
$ kubectl get builds -o=custom-columns-file=./tests/columns.txt
NAME                             TYPE       STATUS    START                  END
test-custom-env-vars             Complete   True      2018-01-26T02:36:00Z   2018-01-26T02:36:02Z
test-custom-volume               Complete   True      2018-01-26T02:36:07Z   2018-01-26T02:36:10Z
test-default-workingdir          Complete   True      2018-01-26T02:36:02Z   2018-01-26T02:36:12Z
test-home-is-set                 Complete   True      2018-01-26T02:35:58Z   2018-01-26T02:36:01Z
test-home-volume                 Complete   True      2018-01-26T02:36:06Z   2018-01-26T02:36:10Z
test-template-duplicate-volume   Invalid    True      <nil>                  <nil>
test-template-volume             Complete   True      2018-01-26T02:36:08Z   2018-01-26T02:36:12Z
test-workingdir                  Complete   True      2018-01-26T02:36:04Z   2018-01-26T02:36:08Z
test-workspace-volume            Complete   True      2018-01-26T02:36:05Z   2018-01-26T02:36:09Z

```

The suite contains a mix of tests that are expected to end in `complete`,
`failed` and `invalid` states, and they are labeled with their expected
end-state, which you can feed into a label selector:

```shell
$ kubectl get builds -o=custom-columns-file=./tests/columns.txt -l expect=invalid
NAME                             TYPE      STATUS    START     END
test-template-duplicate-volume   Invalid   True      <nil>     <nil>

$ kubectl get builds -o=custom-columns-file=./tests/columns.txt -l expect=complete
NAME                      TYPE       STATUS    START                  END
test-custom-env-vars      Complete   True      2018-01-26T02:36:00Z   2018-01-26T02:36:02Z
test-custom-volume        Complete   True      2018-01-26T02:36:07Z   2018-01-26T02:36:10Z
test-default-workingdir   Complete   True      2018-01-26T02:36:02Z   2018-01-26T02:36:12Z
test-home-is-set          Complete   True      2018-01-26T02:35:58Z   2018-01-26T02:36:01Z
test-home-volume          Complete   True      2018-01-26T02:36:06Z   2018-01-26T02:36:10Z
test-template-volume      Complete   True      2018-01-26T02:36:08Z   2018-01-26T02:36:12Z
test-workingdir           Complete   True      2018-01-26T02:36:04Z   2018-01-26T02:36:08Z
test-workspace-volume     Complete   True      2018-01-26T02:36:05Z   2018-01-26T02:36:09Z

```
