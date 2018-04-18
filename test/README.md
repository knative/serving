# Test

This directory contains tests and testing docs for `Elafros`:

* [Unit tests](#running-unit-tests) currently reside in the codebase alongside the code they test.
* [End-to-end tests](#running-end-to-end-tests)
* [Conformance tests](./conformance/README.md) are in [`./test/conformance`](./conformance)

## Running unit tests

The tests can be run using bazel directly:

```shell
bazel test //pkg/... --test_output=errors
```

Or can be run using `go test`:

```shell
go test -v ./pkg/...
```

## Running end-to-end tests

You can either run all the end-to-end tests in an isolate, hermetic cluster, or use your already existing cluster for that.

### Running against an isolate, hermetic cluster

In order to run the end-to-end tests, make sure you:

1. Have `kubetest` installed:
   ```
   wget https://github.com/garethr/kubetest/releases/download/0.1.0/kubetest-darwin-amd64.tar.gz
   tar xf kubetest-darwin-amd64.tar.gz
   cp kubetest /usr/local/bin
   ```
2. Have the `PROJECT_ID` environment variable set to a GCP project you own.

The end-to-end tests can be run by simply executing the `e2e-tests.sh` script.

### Running against an already existing cluster

In order to run the end-to-end tests, make sure you have the `K8S_USER_OVERRIDE`, `K8S_CLUSTER_OVERRIDE` and `DOCKER_REPO_OVERRIDE` environment variables correctly set.

The end-to-end tests can be run by executing `e2e-tests.sh --run-tests` in the command line.

## Running conformance tests

See [conformance test docs](./conformance/README.md).
