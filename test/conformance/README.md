# Conformance tests

Elafros conformance tests can be run against any implementation
of the Elafros API to ensure the API has been implemented consistently.
Passing these tests indicates that apps and functions deployed to
this implementation could be ported to other implementations as well.

_The precedent for these tests is the k8s conformance tests which k8s
vendors use to prove they have ["certified kubernetes"
deployments](https://github.com/cncf/k8s-conformance#certified-kubernetes)._

## Environment requirements

These test require:

* A running `Elafros` cluster up. The tests will use a
  [kubeconfig file](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)
  to determine what `Elafros` cluster to connect to (the `current-context`).
  _See [getting started docs](../../DEVELOPMENT.md#getting-started) to set up
  an `Elafros` environment._
* A docker repo at [`DOCKER_REPO_OVERRIDE`](../../DEVELOPMENT.md#environment-setup)
  that contains [the conformance test images](#conformance-test-images).
  _You will need [`docker`](https://docs.docker.com/install/) to build [the conformance test
  images](#conformance-test-images)._

### Conformance test images

The configuration for the images used for the existing conformance tests lives in
[`test_images_node`](./test_images_node). The images contain a node.js webserver that
will by default listens on port `8080` and expose:

* A service at `/`
* A healthcheck at `/healthz` which can be used for [liveness and readiness probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/)

The two versions of the image ([`Dockerfile.v1`](./test_images_node/Dockerfile.v1),
[`Dockerfile.v2`](./test_images_node/Dockerfile.v2)) differ in the message they return
when you hit `/`.

[`upload-test-images.sh`](./upload-test-images.sh) can be used to build and push the
docker images. It requires:

* [`DOCKER_REPO_OVERRIDE`](../../DEVELOPMENT.md#environment-setup) to be set
* You to be [authenticated with your
  `DOCKER_REPO_OVERRIDE`](../../docs/setting-up-a-docker-registry.md)
* [`docker`](https://docs.docker.com/install/) to be installed

Run it with:

```bash
./test/conformance/upload-test-images.sh
```

## Running conformance tests

All conformance tests can be triggered via `go test`. (You can also use
[the ginkgo cli](https://onsi.github.io/ginkgo/#the-ginkgo-cli).)

You need to have a running environment that meets [the conformance test
environment requirements](#environment-requirements).

To run the conformance tests against the current cluster in `~/.kube/config`:

```bash
go test ./test/conformance
```

Since these tests are fairly slow (~1 minute), [running them tests with
logging enabled](https://onsi.github.io/ginkgo/#logging-output) is useful,
and the Ginkgo output gives a good sense of the progress they are making.
To run with logging enabled use `-v` AND `-ginkgo.v`:

```bash
go test -v ./test/conformance -ginkgo.v
```

By default the tests will use the kubeconfig file at `~./kube/config`.
You can specify a different config file with the argument `--kubeconfig`.

To run the conformance tests with a non-default kubeconfig file:

```bash
go test ./test/conformance -args --kubeconfig /my/path/kubeconfig
```

### Bazel

To run the tests with `bazel` you must:

* Provide a `kubeconfig` file that is a `data` dependency of the test
  ([`BUILD.bazel`](./BUILD.bazel) is configured to use [`./kubeconfig`](./kubeconfig))
* Provide a docker repo from which the built images wil be pulled. This is done
  via the `--dockerrepo` argument.

To run the tests with `bazel` (assuming you have populated [`./kubeconfig`](./kubeconfig)
and your [`DOCKER_REPO_OVERRIDE`](../../DEVELOPMENT.md#environment-setup) is configured
to the location where [you have pushed the conformance test images](#conformance-test-images)):

```bash
bazel test //test/... --test_arg=--dockerrepo=$DOCKER_REPO_OVERRIDE --test_arg=--kubeconfig=./kubeconfig
```

To get `bazel` to run with verbose output:

```bash
bazel test //test/... --test_arg=--dockerrepo=$DOCKER_REPO_OVERRIDE --test_arg=-ginkgo.v
```

## Adding conformance tests

The conformance tests should **ONLY** cover:

  * Functionality that applies to any implementation of the API

The tests must **NOT** require any specific file system permissions to run or
require any additional binaries to be installed in the target environment before
the tests run.

The tests are organized as follows:

* `conformance_suite_test.go` - Contains the logic to invoke all of the test specs
* `${MY_TEST_SUBSET}_test.go` - Contains a set of related specs.

The conformance tests are written using [the `Ginkgo` BDD testing
framework](https://github.com/onsi/ginkgo) which has some nice [getting started
docs](https://onsi.github.io/ginkgo/#getting-started-writing-your-first-test)
if you're new to it.

Each set of specs should be logically grouped, for example you might choose to create
one set of specs to cover one CUJ (critical user journey). For example to use
[ginkgo bootstrapping](https://onsi.github.io/ginkgo/#bootstrapping-a-suite) to
generate a set of test specs for function scaling:

```bash
export MY_TEST_SUBSET=function_scaling
ginkgo generate $MY_TEST_SUBSET
```
