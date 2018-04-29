# Test

This directory contains tests and testing docs for `Elafros`:

* [Unit tests](#running-unit-tests) currently reside in the codebase alongside the code they test
* [Conformance tests](#running-conformance-tests) in [`/test/conformance`](./conformance)
* [End-to-end tests](#running-end-to-end-tests)

If you want to add more tests, see [adding_tests.md](./adding_tests.md).

## Running unit tests

Use bazel:

```shell
bazel test //pkg/... --test_output=errors
```

Or `go test`:

```shell
go test -v ./pkg/...
```
## Running conformance tests

To run [the conformance tests](./conformance), you need to have a running environment that meets
[the conformance test environment requirements](#conformance-test-environment-requirements).

To run the conformance tests against the current cluster in `~/.kube/config`
using `go test` using the environment specified in [your environment
variables](/DEVELOPMENT.md#environment-setup):

Since these tests are fairly slow (~1 minute),  running them with logging
enabled is recommended:

```bash
go test -v ./test/conformance
```

You can [use test flags](#flags) to control the environment
your tests run against, i.e. override [your environment variables](/DEVELOPMENT.md#environment-setup):

```bash
go test -v ./test/conformance --kubeconfig ~/special/kubeconfig --cluster myspecialcluster --dockerrepo myspecialdockerrepo
```

If you are running against an environment with no loadbalancer for the ingress, at the moment
your only option is to use a domain which will resolve to the IP of the running node (see 
[#609](https://github.com/elafros/elafros/issues/609)):

```bash
go test -v ./test/conformance --resolvabledomain
```

## Conformance test environment requirements

These tests require:

1. [A running `Elafros` cluster.](/DEVELOPMENT.md#getting-started)
2. The namespace `pizzaplanet` to exist in the cluster: `kubectl create namespace pizzaplanet`
3. A docker repo contianing [the conformance test images](#conformance-test-images)

### Conformance test images

The configuration for the images used for the existing conformance tests lives in
[`test_images_node`](./conformance/test_images_node).

[`upload-test-images.sh`](./upload-test-images.sh) can be used to build and push the
docker images. It requires:

* [`DOCKER_REPO_OVERRIDE`](/DEVELOPMENT.md#environment-setup) to be set
* You to be [authenticated with your
  `DOCKER_REPO_OVERRIDE`](/docs/setting-up-a-docker-registry.md)
* [`docker`](https://docs.docker.com/install/) to be installed

To run the script:

```bash
./test/conformance/upload-test-images.sh
```

### Running conformance tests with Bazel

To run the conformance tests with `bazel` you must:

* Provide a `kubeconfig` file. This file must be a `data` dependency of the test in
  [`BUILD.bazel`](./conformance/BUILD.bazel). By default [`BUILD.bazel`](./conformance/BUILD.bazel)
  is configured to use [`test/conformance/kubeconfig`](/test/conformance/kubeconfig).
* Provide a docker repo from which the built images will be pulled. This is done
  via the `--dockerrepo` argument.

_The `bazel` execution environment will not contain your environment variables, so you must
explicitly specify them with [command line args](#flags)._

To run the tests with `bazel` (assuming you have populated [`./kubeconfig`](./conformance/kubeconfig)
and your [`DOCKER_REPO_OVERRIDE`](/DEVELOPMENT.md#environment-setup) is configured
to the location where [you have pushed the conformance test images](#conformance-test-images)):

```bash
bazel test //test/... --test_arg=--dockerrepo=$DOCKER_REPO_OVERRIDE --test_arg=--kubeconfig=./kubeconfig
```

## Running end-to-end tests

The script [`e2e-tests.sh`](/test/e2e-tests.sh) can be used to run all the end to end tests
(including [the conformance tests](#running-conformance-tests)) either:

* [Using an existing cluster](#running-against-an-existing-cluster)
* [In an isolated, hermetic GCP cluster](#running-against-an-isolated-hermetic-gcp-cluster)

### Running against an existing cluster

Assuming you have [`K8S_USER_OVERRIDE`, `K8S_CLUSTER_OVERRIDE` and
`DOCKER_REPO_OVERRIDE` set](/DEVELOPMENT.md#environment-setup), run:

```bash
./tests/e2e-tests.sh --run-tests
```

### Running against an isolated, hermetic GCP cluster

This will start a cluster for you in GCP using [kubetest](https://github.com/kubernetes/test-infra/tree/master/kubetest).
Make sure you:

1. Have `kubetest` installed:
   ```
   wget https://github.com/garethr/kubetest/releases/download/0.1.0/kubetest-darwin-amd64.tar.gz
   tar xf kubetest-darwin-amd64.tar.gz
   cp kubetest /usr/local/bin
   ```
2. Have the `PROJECT_ID` environment variable set to a GCP project you own.

Run:

```bash
./tests/e2e-tests.sh
```

## Flags

These flags are useful for running against an existing cluster, making use of your existing
[environment setup])(/DEVELOPMENT.md#environment-setup).

Tests importing [`github.com/elafros/elafros/test`](adding_tests.md#test-library) recognize these flags:

* [`--kubeconfig`](#specifying-kubeconfig)
* [`--cluster`](#specifying-cluster)
* [`--dockerrepo`](#overriding-docker-repo)
* [`--resolvabledomain`](#using-a-resolvable-domain)

#### Specifying kubeconfig

By default the tests will use the [kubeconfig
file](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)
at `~./kube/config`.
You can specify a different config file with the argument `--kubeconfig`.

To run the conformance tests with a non-default kubeconfig file:

```bash
go test ./test/conformance --kubeconfig /my/path/kubeconfig
```

#### Specifying cluster

The `--cluster` argument lets you use a different cluster than [your specified
kubeconfig's](#specifying-kubeconfig) active context. This will default to the value
of your [`K8S_CLUSTER_OVERRIDE` environment variable](/DEVELOPMENT.md#environment-setup)
if not specified.

```bash
go test ./test/conformance --cluster your-cluster-name
```

The current cluster names can be obtained by running:

```bash
kubectl config get-clusters
```

#### Overridding docker repo

The `--dockerrepo` argument lets you specify the docker repo from which images used
by your tests should be pulled. This will default to the value
of your [`DOCKER_REPO_OVERRIDE` environment variable](/DEVELOPMENT.md#environment-setup)
if not specified.

```bash
go test ./test/conformance --dockerrepo gcr.myhappyproject
```

#### Using a resolvable domain

If you setup your cluster using [the getting started
docs](../../DEVELOPMENT.md#getting-started), Routes created in the test will
use the domain `demo-domain.com`, unless the route has label `app=prod` in which
case they will use the domain `prod-domain.com`.  Since these domains will not be
resolvable to deployments in your test cluster, in order to make a request
against the endpoint, the test use the IP assigned to the istio `*-ela-ingress`
and spoof the `Host` in the header.

If you have configured your cluster to use a resolvable domain, you can use the
`--resolvabledomain` flag to indicate that the test should make requests directly against
`Route.Status.Domain` and does not need to spoof the `Host`.
