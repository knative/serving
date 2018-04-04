# Conformance tests

Elafros conformance tests can be run against any implementation
of the Elafros API to ensure the API has been implemented consistently.
Passing these tests indicates that apps and functions deployed to
this implementation could be ported to other implementations as well.

_The precedent for these tests is [the k8s conformance tests](https://github.com/cncf/k8s-conformance)._

## Environment requirements

These tests require:

* **A running `Elafros` cluster.** Specify which cluster to test against using
  [`--kubeconfig`](#specifying-kubeconfig) and/or [`--cluster`](#specifying-cluster).
  _See [getting started docs](../../DEVELOPMENT.md#getting-started) to set up
  an `Elafros` environment._
* **The namespace `pizzaplanet` to exist in the cluster.** To create the namespace:

  ```bash
  cat <<EOF |
  apiVersion: v1
  kind: Namespace
  metadata:
    name: pizzaplanet
  EOF
  kubectl create -f -
  ```

* **[`DOCKER_REPO_OVERRIDE`](../../DEVELOPMENT.md#environment-setup)
  to contain [the conformance test images](#conformance-test-images).**
  _You will need [`docker`](https://docs.docker.com/install/) to build [the conformance test
  images](#conformance-test-images)._

### Conformance test images

The configuration for the images used for the existing conformance tests lives in
[`test_images_node`](./test_images_node). The images contain a node.js webserver that
will by default listens on port `8080` and expose a service at `/`.

The two versions of the image ([`Dockerfile.v1`](./test_images_node/Dockerfile.v1),
[`Dockerfile.v2`](./test_images_node/Dockerfile.v2)) differ in the message they return
when you hit `/`.

[`upload-test-images.sh`](./upload-test-images.sh) can be used to build and push the
docker images. It requires:

* [`DOCKER_REPO_OVERRIDE`](../../DEVELOPMENT.md#environment-setup) to be set
* You to be [authenticated with your
  `DOCKER_REPO_OVERRIDE`](../../docs/setting-up-a-docker-registry.md)
* [`docker`](https://docs.docker.com/install/) to be installed

To run the script:

```bash
./test/conformance/upload-test-images.sh
```

## Running conformance tests

You need to have a running environment that meets [the conformance test
environment requirements](#environment-requirements).

To run the conformance tests against the current cluster in `~/.kube/config`
using `go test`:

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

### Flags

The conformance tests recognize these flags:

* [`--kubeconfig`](#specifying-kubeconfig)
* [`--cluster`](#specifying-cluster)
* [`--resolvabledomain`](#using-a-resolvable-domain)

#### Specifying kubeconfig

By default the tests will use the [kubeconfig
file](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)
at `~./kube/config`.
You can specify a different config file with the argument `--kubeconfig`.

To run the conformance tests with a non-default kubeconfig file:

```bash
go test ./test/conformance -args --kubeconfig /my/path/kubeconfig
```

#### Specifying cluster

The `--cluster` argument lets you use a different cluster than [your specified
kubeconfig's](#specifying-kubeconfig) active context:

```bash
go test ./test/conformance -args --cluster your-cluster-name
```

The current cluster names can be obtained by runing:

```bash
kubectl config get-clusters
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
