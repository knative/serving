# End to end tests

* [Running e2e tests](../README.md#running-e2e-tests)

## Adding end to end tests

Knative Serving e2e tests [test the end to end functionality of the Knative Serving API](#requirements) to verify the behavior of this specific implementation.

These tests use [the test library](../adding_tests.md#test-library).

### Requirements

The e2e tests are used to test whether the flow of Knative Serving is performing as designed from start to finish.

The e2e tests **MUST**: 

1. Provide frequent output describing what actions they are undertaking, especially before performing long running operations.
    1. Log output should be provided exclusively using [the log library](https://golang.org/pkg/log/)
       (vs. [the testing log functions](https://golang.org/pkg/testing/#B.Log), which buffer output until the test has completed).
2. Follow Golang best practices.
