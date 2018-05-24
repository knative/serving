# `ko`

`ko` is an **experimental** CLI to support rapid development of Go containers
with Kubernetes and minimal configuration.

## Installation

`ko` can be installed via:

```shell
go get -u github.com/google/go-containerregistry/cmd/ko
```

## The `ko` Model

`ko` is built around a very simple extension to Go's model for expressing
dependencies using [import paths](https://golang.org/doc/code.html#ImportPaths).

In Go, dependencies are expressed via blocks like:

```go
import (
    "github.com/google/go-containerregistry/authn"
    "github.com/google/go-containerregistry/name"
)
```

Similarly (as you can see above), Go binaries can be referenced via import
paths like `github.com/google/go-containerregistry/cmd/ko`.

**One of the goals of `ko` is to make containers invisible infrastructure.**
Simply replace image references in your Kubernetes yaml with the import path for
your Go binary, and `ko` will handle containerizing and publishing that
container image as needed.

For example, you might use the following in a Kubernetes `Deployment` resource:

```yaml
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  name: hello-world
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: hello-world
        # This is the import path for the Go binary to build and run.
        image: github.com/mattmoor/examples/http/cmd/helloworld
        ports:
        - containerPort: 8080
```

### Determining supported import paths

Similar to other tooling in the Go ecosystem, `ko` expects to execute in the
context of your `$GOPATH`. This is used to determine what package(s) `ko`
is expected to build.

Suppose `GOPATH` is `~/gopath` and the current directory is
`~/gopath/src/github.com/mattmoor/examples`. `ko` will deduce the base import
path to be `github.com/mattmoor/examples`, and any references to subpackages
of this will be built, containerized and published.

For example, any of the following would be matched:
* `github.com/mattmoor/examples`
* `github.com/mattmoor/examples/cmd/foo`
* `github.com/mattmoor/examples/bar`

`ko` does not (currently) support `vendor`d binaries, or building other
binaries from `GOPATH` (outside of the current scope).

### Results

Employing this convention enables `ko` to have effectively zero configuration
and enable very fast development iteration. For
[warm-image](https://github.com/mattmoor/warm-image), `ko` is able to
build, containerize, and redeploy a non-trivial Kubernetes controller app in
roughly 10 seconds (dominated by two `go build`s).

```shell
~/go/src/github.com/mattmoor/warm-image$ ko apply -f config/
2018/04/25 17:11:28 Go building github.com/mattmoor/warm-image/cmd/sleeper
2018/04/25 17:11:28 Go building github.com/mattmoor/warm-image/cmd/controller
2018/04/25 17:11:29 Publishing gcr.io/my-project/github.com/mattmoor/warm-image/cmd/sleeper:latest
2018/04/25 17:11:30 mounted sha256:eb05f3dbdb543cc610527248690575bacbbcebabe6ecf665b189cf18b541e3ca
2018/04/25 17:11:30 mounted sha256:3ca7d60fa89dc8a1faea3046fd3516f23dab93489f3888ae539df4abc0973e52
2018/04/25 17:11:30 mounted sha256:e2cc7c829942a768015dbcfdad7205c104cb85b84a79573555a8f4381b98110c
2018/04/25 17:11:30 pushed gcr.io/my-project/github.com/mattmoor/warm-image/cmd/sleeper:latest
2018/04/25 17:11:30 Published gcr.io/my-project/github.com/mattmoor/warm-image/cmd/sleeper@sha256:193acdbeff1ea9f105f49d97a6ceb7adbd30b3d64a8b9949382f4be9569cd06d
2018/04/25 17:11:37 Publishing gcr.io/my-project/github.com/mattmoor/warm-image/cmd/controller:latest
2018/04/25 17:11:37 mounted sha256:dc0dd55edef1443e976c835825479c3dc713bb689547f8a170f8a0d14f9ff734
2018/04/25 17:11:37 mounted sha256:eb05f3dbdb543cc610527248690575bacbbcebabe6ecf665b189cf18b541e3ca
2018/04/25 17:11:37 mounted sha256:fbc44e14a1d848ed485b5c3f03611c3e21aaa197fcba579255e5f8416a1b7172
2018/04/25 17:11:38 pushed gcr.io/my-project/github.com/mattmoor/warm-image/cmd/controller:latest
2018/04/25 17:11:38 Published gcr.io/my-project/github.com/mattmoor/warm-image/cmd/controller@sha256:78794915fca48d0c4b339dc1df91a72f1e4bc6a7b33beaeef8aecda0947d5d31
clusterrolebinding "warmimage-controller-admin" configured
deployment "warmimage-controller" unchanged
namespace "warmimage-system" configured
serviceaccount "warmimage-controller" unchanged
customresourcedefinition "warmimages.mattmoor.io" configured
```

## Usage

`ko` has four commands:

### `ko publish`

`ko publish` simply builds and publishes images for each import path passed as
an argument. It prints the images' published digests after each image is published.

```shell
$ ko publish github.com/mattmoor/warm-image/cmd/sleeper
2018/04/25 17:11:28 Go building github.com/mattmoor/warm-image/cmd/sleeper
2018/04/25 17:11:29 Publishing gcr.io/my-project/github.com/mattmoor/warm-image/cmd/sleeper:latest
2018/04/25 17:11:30 mounted sha256:eb05f3dbdb543cc610527248690575bacbbcebabe6ecf665b189cf18b541e3ca
2018/04/25 17:11:30 mounted sha256:3ca7d60fa89dc8a1faea3046fd3516f23dab93489f3888ae539df4abc0973e52
2018/04/25 17:11:30 mounted sha256:e2cc7c829942a768015dbcfdad7205c104cb85b84a79573555a8f4381b98110c
2018/04/25 17:11:30 pushed gcr.io/my-project/github.com/mattmoor/warm-image/cmd/sleeper:latest
2018/04/25 17:11:30 Published gcr.io/my-project/github.com/mattmoor/warm-image/cmd/sleeper@sha256:193acdbeff1ea9f105f49d97a6ceb7adbd30b3d64a8b9949382f4be9569cd06d
```

To determine where to publish the images, `ko` currently requires the
environment variable `KO_DOCKER_REPO` to be set to an acceptable docker
repository (e.g. `gcr.io/your-project`). **This will likely change in a
future version.**

### `ko resolve`

`ko resolve` takes Kubernetes yaml files in the style of `kubectl apply`
and (based on the [model above](#the-ko-model)) determines the set of
Go import paths to build, containerize, and publish.

The output of `ko resolve` is the concatenated yaml with import paths
replaced with published image digests. Following the example above,
this would be:

```shell
# Command
export PROJECT_ID=$(gcloud config get-value core/project)
export KO_DOCKER_REPO="gcr.io/${PROJECT_ID}"
ko resolve -f deployment.yaml

# Output
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  name: hello-world
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: hello-world
        # This is the digest of the published image containing the go binary
        # at the embedded import path.
        image: gcr.io/your-project/github.com/mattmoor/examples/http/cmd/helloworld@sha256:deadbeef
        ports:
        - containerPort: 8080
```

*Note that DockerHub does not currently support these multi-level names. We may
employ alternate naming strategies in the future to broaden support, but this
would sacrifice some amount of identifiability.*

### `ko apply`

`ko apply` is intended to parallel `kubectl apply`, but acts on the same
resolved output as `ko resolve` emits. It is expected that `ko apply` will act
as the vehicle for rapid iteration during development. As changes are made to a
particular application, you can run: `ko apply -f unit.yaml` to rapidly
rebuild, repush, and redeploy their changes.

`ko apply` will invoke `kubectl apply` under the covers, and therefore apply
to whatever `kubectl` context is active.

### `ko delete`

`ko delete` simply passes through to `kubectl delete`. It is exposed purely out
of convenience for cleaning up resources created through `ko apply`.

## Configuration via `.ko.yaml`

While `ko` aims to have zero configuration, there are certain scenarios where
you will want to override `ko`'s default behavior. This is done via `.ko.yaml`.

`.ko.yaml` is put into the directory from which `ko` will be invoked. If it
is not present, then `ko` will rely on its default behaviors.

### Overriding the default base image

By default, `ko` makes use of `gcr.io/distroless/base:latest` as the base image
for containers. There are a wide array of scenarios in which overriding this
makes sense, for example:
1. Pinning to a particular digest of this image for repeatable builds,
1. Replacing this streamlined base image with another with better debugging
  tools (e.g. a shell, like `docker.io/library/ubuntu`).

The default base image `ko` uses can be changed by simply adding the following
line to `.ko.yaml`:

```yaml
defaultBaseImage: gcr.io/another-project/another-image@sha256:deadbeef
```

### Overriding the base for particular imports

Some of your binaries may have requirements that are a more unique, and you
may want to direct `ko` to use a particular base image for just those binaries.

The base image `ko` uses can be changed by adding the following to `.ko.yaml`:

```yaml
baseImageOverrides:
  github.com/my-org/my-repo/path/to/binary: docker.io/another/base:latest
```

### Why isn't `KO_DOCKER_REPO` part of `.ko.yaml`?

Once introduced to `.ko.yaml`, you may find yourself wondering: Why does it
not hold the value of `$KO_DOCKER_REPO`?

The answer is that `.ko.yaml` is expected to sit in the root of a repository,
and get checked in and versioned alongside your source code. This also means
that the configured values will be shared across developers on a project, which
for `KO_DOCKER_REPO` is actually undesireable because each developer is (likely)
using their own docker repository and cluster.


## Relevance to Release Management

`ko` is also useful for helping manage releases. For example, if your project
periodically releases a set of images and configuration to launch those images
on a Kubernetes cluster, release binaries may be published and the configuration
generated via:

```shell
export PROJECT_ID=<YOUR RELEASE PROJECT>
export KO_DOCKER_REPO="gcr.io/${PROJECT_ID}"
ko resolve -f config/ > release.yaml
```

This will publish all of the binary components as container images to
`gcr.io/my-releases/...` and create a `release.yaml` file containing all of the
configuration for your application with inlined image references.

This resulting configuration may then be installed onto Kubernetes clusters via:

```shell
kubectl apply -f release.yaml
```


## Acknowledgements

This work is based heavily on learnings from having built the
[Docker](https://github.com/bazelbuild/rules_docker) and
[Kubernetes](https://github.com/bazelbuild/rules_k8s) support for
[Bazel](https://bazel.build). That work was presented
[here](https://www.youtube.com/watch?v=RS1aiQqgUTA).
