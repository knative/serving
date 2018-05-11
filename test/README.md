# Test

This directory contains tests and testing docs for `Elafros`:

* [Unit tests](#running-unit-tests) currently reside in the codebase alongside the code they test
* [Conformance tests](#running-conformance-tests) in [`/test/conformance`](./conformance)
* [End-to-end tests](#running-end-to-end-tests) in [`/test/e2e`](./e2e)

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

Since these tests are fairly slow (~1 minute), running them with logging
enabled is recommended.

To run the conformance tests against the current cluster in `~/.kube/config`
use `go test` with test caching disabled and the environment specified in [your environment
variables](/DEVELOPMENT.md#environment-setup):

```bash
go test -v -count=1 ./test/conformance
```

You can [use test flags](#flags) to control the environment
your tests run against, i.e. override [your environment variables](/DEVELOPMENT.md#environment-setup):

```bash
go test -v -count=1 ./test/conformance --kubeconfig ~/special/kubeconfig --cluster myspecialcluster --dockerrepo myspecialdockerrepo
```

If you are running against an environment with no loadbalancer for the ingress, at the moment
your only option is to use a domain which will resolve to the IP of the running node (see 
[#609](https://github.com/elafros/elafros/issues/609)):

```bash
go test -v -count=1 ./test/conformance --resolvabledomain
```

## Conformance test environment requirements

These tests require:

1. [A running `Elafros` cluster.](/DEVELOPMENT.md#getting-started)
2. The namespace `pizzaplanet` to exist in the cluster: `kubectl create namespace pizzaplanet`
3. A docker repo contianing [the conformance test images](#conformance-test-images)

### Conformance test images

The configuration for the images used for the existing conformance tests lives in
[`test_images_node`](./conformance/test_images_node). See the [section about test
images](#test-images) for details about building and adding new ones.

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

The e2e tests have almost the exact same requirements and specs as the conformance tests, but they will be enumerated for clarity.

To run [the e2e tests](./e2e), you need to have a running environment that meets
[the e2e test environment requirements](#e2e-test-environment-requirements).

To run the e2e tests against the current cluster in `~/.kube/config`
using `go test` using the environment specified in [your environment
variables](/DEVELOPMENT.md#environment-setup):

Since these tests are fairly slow,  running them with logging
enabled is recommended. Do so by passing the `-v` flag to go test like so: 

```bash
go test -v ./test/e2e
```

You can [use test flags](#flags) to control the environment
your tests run against, i.e. override [your environment variables](/DEVELOPMENT.md#environment-setup):

```bash
go test -v ./test/e2e --kubeconfig ~/special/kubeconfig --cluster myspecialcluster --dockerrepo myspecialdockerrepo
```

If you are running against an environment with no loadbalancer for the ingress, at the moment
your only option is to use a domain which will resolve to the IP of the running node (see 
[#609](https://github.com/elafros/elafros/issues/609)):

```bash
go test -v ./test/e2e --resolvabledomain
```

## End-to-end test environment requirements

These tests require:

1. [A running `Elafros` cluster.](/DEVELOPMENT.md#getting-started)
2. The namespace `noodleburg` to exist in the cluster: `kubectl create namespace noodleburg`
3. A docker repo containing [the e2e test images](#e2e-test-images)

### End-to-end test images

The configuration for the images used for the existing e2e tests lives in
[`test_images_node`](./e2e/test_images_node). See the [section about test
images](#test-images) for details about building and adding new ones.

### Running e2e tests with Bazel

To run the e2e tests with `bazel` you must:

* Provide a `kubeconfig` file. This file must be a `data` dependency of the test in
  [`BUILD.bazel`](./e2e/BUILD.bazel). By default [`BUILD.bazel`](./e2e/BUILD.bazel)
  is configured to use [`test/e2e/kubeconfig`](/test/e2e/kubeconfig).
* Provide a docker repo from which the built images will be pulled. This is done
  via the `--dockerrepo` argument.

_The `bazel` execution environment will not contain your environment variables, so you must
explicitly specify them with [command line args](#flags)._

To run the tests with `bazel` (assuming you have populated [`./kubeconfig`](./e2e/kubeconfig)
and your [`DOCKER_REPO_OVERRIDE`](/DEVELOPMENT.md#environment-setup) is configured
to the location where [you have pushed the e2e test images](#e2e-test-images)):

```bash
bazel test //test/... --test_arg=--dockerrepo=$DOCKER_REPO_OVERRIDE --test_arg=--kubeconfig=./kubeconfig
```

## Test images

### Building the test images

The [`upload-test-images.sh`](./upload-test-images.sh) script can be used to build and push the
test images used by the conformance and e2e tests. It requires:

* [`DOCKER_REPO_OVERRIDE`](/DEVELOPMENT.md#environment-setup) to be set
* You to be [authenticated with your
  `DOCKER_REPO_OVERRIDE`](/docs/setting-up-a-docker-registry.md)
* [`docker`](https://docs.docker.com/install/) to be installed

To run the script:

```bash
./test/upload-test-images.sh /path/containing/test/images
```

The path containing test images is any directory whose subdirectories contain the `Dockerfile`
and any required files to build Docker images (e.g., `./test/e2e/test_images_node`).

### Adding new test images

New test images should be placed in their own subdirectories. Be sure to to include a `Dockerfile`
for building and running the test image.

The new test images will also need to be uploaded to the e2e tests Docker repo. You will need one
of the owners found in [`/test/OWNERS`](OWNERS) to do this.

## Flags

These flags are useful for running against an existing cluster, making use of your existing
[environment setup](/DEVELOPMENT.md#environment-setup).

Tests importing [`github.com/elafros/elafros/test`](adding_tests.md#test-library) recognize these flags:

* [`--kubeconfig`](#specifying-kubeconfig)
* [`--cluster`](#specifying-cluster)
* [`--dockerrepo`](#overriding-docker-repo)
* [`--resolvabledomain`](#using-a-resolvable-domain)

#### Specifying kubeconfig

By default the tests will use the [kubeconfig
file](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)
at `~/.kube/config`.
You can specify a different config file with the argument `--kubeconfig`.

To run the tests with a non-default kubeconfig file:

```bash
go test ./test/conformance --kubeconfig /my/path/kubeconfig
```

```bash
go test ./test/e2e --kubeconfig /my/path/kubeconfig
```
#### Specifying cluster

The `--cluster` argument lets you use a different cluster than [your specified
kubeconfig's](#specifying-kubeconfig) active context. This will default to the value
of your [`K8S_CLUSTER_OVERRIDE` environment variable](/DEVELOPMENT.md#environment-setup)
if not specified.

```bash
go test ./test/conformance --cluster your-cluster-name
```

```bash
go test ./test/e2e --cluster your-cluster-name
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

```bash
go test ./test/e2e --dockerrepo gcr.myhappyproject
```

#### Using a resolvable domain

If you set up your cluster using [the getting started
docs](/DEVELOPMENT.md#getting-started), Routes created in the test will
use the domain `demo-domain.com`, unless the route has label `app=prod` in which
case they will use the domain `prod-domain.com`.  Since these domains will not be
resolvable to deployments in your test cluster, in order to make a request
against the endpoint, the test use the IP assigned to the istio `*-ela-ingress`
and spoof the `Host` in the header.

If you have configured your cluster to use a resolvable domain, you can use the
`--resolvabledomain` flag to indicate that the test should make requests directly against
`Route.Status.Domain` and does not need to spoof the `Host`.
