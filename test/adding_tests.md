# Adding tests

If you are [developing knative](../DEVELOPMENT.md) you may need to add or
change:

- [e2e tests](./e2e)
- [Conformance tests](./conformance)

Both tests can use our [test library](#test-library).

Reviewers of conformance and e2e tests (i.e. [OWNERS](./OWNERS)) are responsible
for the style and quality of the resulting tests. In order to not discourage
contributions, when style change are required, the reviewers can make the
changes themselves.

All e2e and conformance tests _must_ be marked with the `e2e`
[build constraint](https://golang.org/pkg/go/build/) so that `go test ./...` can
be used to run only [the unit tests](README.md#running-unit-tests), i.e.:

```go
// +build e2e
```

## Test library

In the [`test`](.) dir you will find several libraries in the `test` package you
can use in your tests.

This library exists partially in this directory and partially in
[`knative/pkg/test`](https://github.com/knative/pkg/tree/master/test).

The libs in this dir can:

- [Get access to client objects](#get-access-to-client-objects)
- [Make requests against deployed services](#make-requests-against-deployed-services)
- [Check Knative Serving resources](#check-knative-serving-resources)
- [Verify resource state transitions](#verify-resource-state-transitions)
- [Generate boilerplate CRDs](#generate-boilerplate-crds)

See [`knative/pkg/test`](https://github.com/knative/pkg/tree/master/test) to:

- [Use common test flags](#use-common-test-flags)
- Output logs
- Emit metrics
- Ensure test cleanup

### Use common test flags

These flags are useful for running against an existing cluster, making use of
your existing [environment setup](../DEVELOPMENT.md#setup-your-environment).

By importing `knative.dev/pkg/test` you get access to a global variable called
`test.Flags` which holds the values of
[the command line flags](./README.md#flags).

```go
imagePath := strings.Join([]string{test.Flags.DockerRepo, image}, "/"))
```

_See
[e2e_flags.go](https://github.com/knative/pkg/blob/master/test/e2e_flags.go)._

### Get access to client objects

To initialize client objects that you can use the command line flags that
describe the environment:

```go
import (
	testing

	knative.dev/serving/test
	pkgTest "knative.dev/pkg/test"
)

func Setup(t *testing.T) *test.Clients {
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, namespaceName)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}
```

The `Clients` struct contains initialized clients for accessing:

- `Kubernetes objects`
- `Services`
- `Routes`
- `Configurations`
- `Revisions`
- `Knative ingress`
- `ServerlessServices`
- `Istio objects`

For example, to create a `Route`:

```go
_, err = clients.ServingClient.Routes.Create(v1test.Route(
	test.ResourceNames{
		Route:  routeName,
		Config: configName,
	}))
```

_v1test is alias for package `knative.dev/serving/pkg/testing/v1`_

And you can use the client to clean up `Route` and `Configuration` resources
created by your test:

```go
import "knative.dev/serving/test"

func tearDown(clients *test.Clients) {
	if clients != nil {
		clients.ServingClient.Routes.Delete(routeName, nil)
		clients.ServingClient.Configs.Delete(configName, nil)
	}
}
```

_See [clients.go](./clients.go)._

### Make requests against deployed services

After deploying (i.e. creating a `Route` and a `Configuration`) an endpoint will
not be ready to serve requests right away. To poll a deployed endpoint and wait
for it to be in the state you want it to be in (or timeout) use
`WaitForEndpointState` by importing `knative.dev/pkg/test` with alias `pkgTest`:

```go
_, err := pkgTest.WaitForEndpointState(
	clients.KubeClient,
	logger,
	updatedRoute.Status.URL.URL(),
	pkgTest.EventuallyMatchesBody(expectedText),
	"SomeDescription",
	test.ServingFlags.ResolvableDomain)
if err != nil {
	t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", routeName, updatedRoute.Status.Domain, expectedText, err)
}
```

This function makes use of
[the environment flag `resolvableDomain`](README.md#using-a-resolvable-domain)
to determine if the ingress should be used or the domain should be used
directly.

_See [request.go](https://github.com/knative/pkg/blob/master/test/request.go)._

If you need more low-level access to the http request or response against a
deployed service, you can directly use the `SpoofingClient` that
`WaitForEndpointState` wraps.

```go
// Error handling elided for brevity, but you know better.
client, err := pkgTest.NewSpoofingClient(clients.KubeClient.Kube, logger, route.Status.Domain, test.ServingFlags.ResolvableDomain)
req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", route.Status.Domain), nil)

// Single request.
resp, err := client.Do(req)

// Polling until we meet some condition.
resp, err := client.Poll(req, test.BodyMatches(expectedText))
```

_See
[spoof.go](https://github.com/knative/pkg/blob/master/test/spoof/spoof.go)._

### Check Knative Serving resources

After creating Knative Serving resources or making changes to them, you will
need to wait for the system to realize those changes. You can use the Knative
Serving CRD check and polling methods to check the resources are either in or
reach the desired state.

The `WaitFor*` functions use the kubernetes
[`wait` package](https://godoc.org/k8s.io/apimachinery/pkg/util/wait). To poll
they use
[`PollImmediate`](https://godoc.org/k8s.io/apimachinery/pkg/util/wait#PollImmediate)
and the return values of the function you provide behave the same as
[`ConditionFunc`](https://godoc.org/k8s.io/apimachinery/pkg/util/wait#ConditionFunc):
a `bool` to indicate if the function should stop or continue polling, and an
`error` to indicate if there has been an error.

For example, you can poll a `Configuration` object to find the name of the
`Revision` that was created for it:

```go
var revisionName string
err := v1alpha1testing.WaitForConfigurationState(clients.ServingClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
	if c.Status.LatestCreatedRevisionName != "" {
		revisionName = c.Status.LatestCreatedRevisionName
		return true, nil
	}
	return false, nil
}, "ConfigurationUpdatedWithRevision")
```

_v1alpha1testing is alias for package
`knative.dev/serving/pkg/testing/v1alpha1`_

We also have `Check*` variants of many of these methods with identical
signatures, same example:

```go
var revisionName string
err := v1alpha1testing.CheckConfigurationState(clients.ServingClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
	if c.Status.LatestCreatedRevisionName != "" {
		revisionName = c.Status.LatestCreatedRevisionName
		return true, nil
	}
	return false, nil
})
```

_v1alpha1testing is alias for package
`knative.dev/serving/pkg/testing/v1alpha1`_

_For knative crd state, for example `Config`. You can see the code in
[configuration.go](./v1alpha1/configuration.go). For kubernetes objects see
[kube_checks.go](https://github.com/knative/pkg/blob/master/test/kube_checks.go)._

### Verify resource state transitions

To use the [check functions](#check-knative-serving-resources) you must provide
a function to check the state. Some of the expected transition states (as
defined in
[the Knative Serving spec](https://github.com/knative/docs/blob/master/docs/serving/spec/knative-api-specification-1.0.md))
, for example `v1alpha1/Revision` state, are expressed in function in
[revision.go](./v1alpha1/revision.go).

For example when a `Revision` has been created, the system will start the
resources required to actually serve it, and then the `Revision` object will be
updated to indicate it is ready. This can be polled with
`v1alpha1testing.IsRevisionReady`:

```go
err := v1alpha1testing.WaitForRevisionState(clients.ServingAlphaClient, revName, v1alpha1testing.IsRevisionReady, "RevisionIsReady")
if err != nil {
	t.Fatalf("The Revision %q did not become ready: %v", revName, err)
}
```

_v1alpha1testing is alias for package
`knative.dev/serving/pkg/testing/v1alpha1`_

Once the `Revision` is created, all traffic for a `Route` should be routed to
it. This can be polled with `v1alpha1testing.AllRouteTrafficAtRevision`:

```go
err := v1alpha1testing.CheckRouteState(clients.ServingAlphaClient, names.Route, v1alpha1testing.AllRouteTrafficAtRevision(names))
if err != nil {
	t.Fatalf("The Route %s was not updated to route traffic to the Revision %s: %v", names.Route, names.Revision, err)
}
```

_See [route.go](./v1alpha1/route.go)._

### Generate boilerplate CRDs

Your tests will probably need to create `Route` and `Configuration` objects. You
can use the existing boilerplate to describe them.

You can also use the function `AppendRandomString` to create a random name for
your `crd` so that your tests can use unique names each time they run.

For example to create a `Configuration` object that uses a certain docker image
with a randomized name:

```go
func TestSomeAwesomeFeature(t *testing.T) {
	var names test.ResourceNames
	names.Config := test.ObjectNameForTest(t)
	_, err := clients.ServingClient.Create(test.Configuration(namespaceName, names, imagePath))
	if err != nil {
		// handle error case
	}
	// more testing
}
```

_test is package `knative.dev/serving/test`_

Please expand these functions as more use cases are tested.

_See [crd.go](./crd.go)._
