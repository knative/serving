# Certificate Conformance Testing

## Adding a test

Tests need to be exported and accessible downstream so they should be placed in
non-test files (ie. sometest.go). Additionally, invoke your test in the default
`RunConformance` function in [`run.go`](./http01/run.go). This function is the
entry point by which tests are executed.

This approach aims to reduce the changes required when tests are added &
removed.

## Running the tests downstream

To run all the conformance tests in your own repo we encourage adopting the
[`RunConformance`](./run.go) function to run all your tests.

To do so would look something like:

```go
package conformance

import (
	"testing"
	"knative.dev/serving/test/conformance/certificate/http01"
)

func TestYourHTTP01ProviderConformance(t *testing.T) {
	http01.RunConformance(t)
}

```
