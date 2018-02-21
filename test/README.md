# Test

This directory contains tests and testing docs for `Elafros`:

* Unit tests currently reside in the codebase alongside the code they test.
* [Conformance tests](./conformance/README.md) are in [`./test/conformance`](./conformance)

## Running unit tests

Run unit tests with the [`unit-tests.sh`](./unit-tests.sh) script:

```shell
./test/unit-tests.sh
```

The tests can also be run using bazel directly:

```shell
bazel test //pkg/... --test_output=errors
```

Or can be run using `go test`:

```shell
go test -v ./pkg/...
```

## Running conformance tests

See [conformance test docs](./conformance/README.md).