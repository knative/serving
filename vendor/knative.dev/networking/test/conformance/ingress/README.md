# Ingress Conformance Testing

This directory contains Ingress conformance tests for Knative Ingress resource.

## Environment requirements

### Development tools

1. [`go`](https://golang.org/doc/install): The language `Knative Serving` is
   built in (1.13 or later)
1. [`ko`](https://github.com/google/ko): Build tool to setup the environment.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For
   managing development environments.

### Test environment

1. [A running `Knative Serving` cluster.](https://github.com/knative/serving/blob/master/DEVELOPMENT.md#prerequisites),
   with the Ingress implementation of choice installed.
   ```bash
   # Set the Ingress class annotation to use in tests.
   # Some examples:
   #   export INGRESS_CLASS=gloo.ingress.networking.knative.dev      # Gloo Ingress
   #   export INGRESS_CLASS=istio.ingress.networking.knative.dev     # Istio Ingress
   #   export INGRESS_CLASS=kourier.ingress.networking.knative.dev   # Kourier Ingress
   export INGRESS_CLASS=<your-ingress-class-annotation>
   ```
1. Knative Networking source code check out at `${NETWORKING_ROOT}`. Often this
   is `$GOPATH/src/go/knative.dev/networking`. This contains both the test
   images and the tests.
   ```bash
   export NETWORKING_ROOT=<where-you-checked-out-knative/networking>
   ```
1. (Recommended) Knative
   [net-istio](https://github.com/knative-sandbox/net-istio) source code checked
   out. This contains an invocation of `RunConformance` that easily allows to
   run tests.
1. (For setup only) Knative Serving source code check out at `${SERVING_ROOT}`.
   Often this is `$GOPATH/src/go/knative.dev/serving`. This contains the
   `knative-testing` resources.
   ```bash
   export SERVING_ROOT=<where-you-checked-out-knative/serving>
   ```
1. A docker repo containing [the test images](#test-images) `KO_DOCKER_REPO`:
   The docker repository to which developer images should be pushed (e.g.
   `gcr.io/[gcloud-project]`).

   ```bash
   export KO_DOCKER_REPO=<your-docker-repository>
   ```

1. The `knative-testing` resources

   ```bash
   ko apply -f "$SERVING_ROOT/test/config"
   ```

## Building the test images

NOTE: this is only required when you run conformance/e2e tests locally with
`go test` commands, and may be required periodically.

The [`upload-test-images.sh`](../../upload-test-images.sh) script can be used to
build and push the test images used by the conformance and e2e tests. The script
expects your environment to be setup as described in
[DEVELOPMENT.md](https://github.com/knative/serving/blob/master/DEVELOPMENT.md#install-requirements).

To run the script for all end to end test images:

```bash
cd $NETWORKING_ROOT
./test/upload-test-images.sh
```

## Adding a test

Tests need to be exported and accessible downstream so they should be placed in
non-test files (ie. sometest.go). Additionally, invoke your test in the default
`RunConformance` function in [`run.go`](./run.go). This function is the entry
point by which tests are executed.

This approach aims to reduce the changes required when tests are added &
removed.

## Running the tests

### Running the tests downstream

To run all the conformance tests in your own repo we encourage adopting the
[`RunConformance`](./run.go) function to run all your tests.

To do so would look something like:

```go
package conformance

import (
	"testing"
	"knative.dev/serving/test/conformance/ingress"
)

func TestYourIngressConformance(t *testing.T) {
	ingress.RunConformance(t)
}
```

### Running the tests from `net-istio` repository

`net-istio` already invokes the `RunConformance` function in
[`ingress_test.go`](https://github.com/knative-sandbox/net-istio/blob/master/test/conformance/ingress_test.go),
so it offers a convenient place to run the tests.

If `INGRESS_CLASS` is already set, then you can simply `go test ingress_test.go`

## How to run tests from your local repository

1. Clone the net-istio repository (or use any repository that invokes
   [`RunConformance`](./run.go)).
1. In net-istio, add an entry to go.mod that points to your local networking
   folder:


1. Make any changes to your local networking E2E tests
1. Run `go mod vendor` in net-istio
1. Run `go test test/conformance/ingress_test.go`

NOTE: You will need to run `go mod vendor` for every change you make.
```
