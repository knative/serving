# Adding tests

If you are [developing knative](/DEVELOPMENT.md) you may need to add or change:

* [e2e tests](./e2e)
* [Conformance tests](./conformance)

Both tests can use our [test library](#test-library).

Reviewers of conformance and e2e tests (i.e. [OWNERS](/test/OWNERS)) are responsible for the style and quality of the resulting tests. In order to not discourage contributions, when style change are required, the reviewers can make the changes themselves.

All e2e and conformance tests _must_ be marked with the `e2e` [build constraint](https://golang.org/pkg/go/build/)
so that `go test ./...` can be used to run only [the unit tests](README.md#running-unit-tests), i.e.:

```go
// +build e2e
```

## Test library

In the [`test`](/test/) dir you will find several libraries in the `test` package
you can use in your tests.

You can:

* [Use common test flags](#use-common-test-flags)
* [Output log](#output-log)
* [Get access to client objects](#get-access-to-client-objects)
* [Make requests against deployed services](#make-requests-against-deployed-services)
* [Check Knative Serving resources](#check-knative-serving-resources)
* [Verify resource state transitions](#verify-resource-state-transitions)
* [Generate boilerplate CRDs](#generate-boilerplate-crds)
* [Ensure test cleanup](#ensure-test-cleanup)

### Use common test flags

These flags are useful for running against an existing cluster, making use of your existing
[environment setup](/DEVELOPMENT.md#environment-setup).

By importing `github.com/knative/serving/test` you get access to a global variable called
`test.Flags` which holds the values of [the command line flags](/test/README.md#flags).

```go
imagePath := strings.Join([]string{test.Flags.DockerRepo, image}, "/"))
```

_See [e2e_flags.go](./e2e_flags.go)._

### Output log

We are using [Knative logging library](/pkg/logging) for structured logging, it is built on top of [zap](https://github.com/uber-go/zap).
All test case should define its own logger with test case name as logger name, and pass it around; this way, all output from the test case will be tagged by same logger name.
Pls see below for sample code from test case [`TestHelloWorld`](./e2e/helloworld_test.go)
`logger.Debug` outputs debug log. Pls _see [errorcondition_test.go](./e2e/errorcondition_test.go)._ for an example of `logger.Debug()` call.
When tests are run with `--logverbose` option, debug logs will show.

```go
func TestHelloWorld(t *testing.T) {
    clients := Setup(t)
    //add test case specific name to its own logger
    logger := test.Logger.Named("TestHelloWorld")
    var imagePath string
    imagePath = strings.Join([]string{test.Flags.DockerRepo, "helloworld"}, "/")
    logger.Infof("Creating a new Route and Configuration")
    ...
}
```

### Get access to client objects

To initialize client objects that you can use [the command line flags](#use-flags)
that describe the environment:

```go
func setup(t *testing.T) *test.Clients {
    clients, err := test.NewClients(kubeconfig, cluster, namespaceName)
    if err != nil {
        t.Fatalf("Couldn't initialize clients: %v", err)
    }
    return clients
}
```

The `Clients` struct contains initialized clients for accessing:

* Kubernetes objects
* `Routes`
* `Configurations`
* `Revisions`

For example, to create a `Route`:

```bash
_, err = clients.Routes.Create(test.Route(namespaceName, routeName, configName))
```

And you can use the client to clean up `Route` and `Configuration` resources created
by your test:

```go
func tearDown(clients *test.Clients) {
    if clients != nil {
        clients.Delete([]string{routeName}, []string{configName})
    }
}
```

_See [clients.go](./clients.go)._

### Make requests against deployed services

After deploying (i.e. creating a `Route` and a `Configuration`) an endpoint will not be
ready to serve requests right away. To poll a deployed endpoint and wait for it to be
in the state you want it to be in (or timeout) use `WaitForEndpointState`:

```go
err = test.WaitForEndpointState(clients.Kube, resolvableDomain, updatedRoute.Status.Domain, namespaceName, routeName, func(body string) (bool, error) {
    return body == expectedText, nil
})
if err != nil {
    t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", routeName, updatedRoute.Status.Domain, expectedText, err)
}
```

This function makes use of [the environment flag `resolvableDomain`](#use-flags) to determine if the ingress
should be used or the domain should be used directly.

_See [request.go](./request.go)._

### Check Knative Serving resources

After creating Knative Serving resources or making changes to them, you will need to wait for the system
to realize those changes. You can use the Knative Serving CRD check and polling methods to check the
resources are either in or reach the desired state.

The `WaitFor*` functions use the kubernetes [`wait` package](https://godoc.org/k8s.io/apimachinery/pkg/util/wait).
To poll they use [`PollImmediate`](https://godoc.org/k8s.io/apimachinery/pkg/util/wait#PollImmediate)
and the return values of the function you provide behave the same as
[`ConditionFunc`](https://godoc.org/k8s.io/apimachinery/pkg/util/wait#ConditionFunc):
a `bool` to indicate if the function should stop or continue polling, and an `error` to indicate if
there has been an error.

For example, you can poll a `Configuration` object to find the name of the `Revision` that was created
for it:

```go
var revisionName string
err := test.WaitForConfigurationState(clients.Configs, configName, func(c *v1alpha1.Configuration) (bool, error) {
    if c.Status.LatestCreatedRevisionName != "" {
        revisionName = c.Status.LatestCreatedRevisionName
        return true, nil
    }
    return false, nil
})
```

We also have `Check*` variants of many of these methods with identical signatures, same example:

```go
var revisionName string
err := test.CheckConfigurationState(clients.Configs, configName, func(c *v1alpha1.Configuration) (bool, error) {
    if c.Status.LatestCreatedRevisionName != "" {
        revisionName = c.Status.LatestCreatedRevisionName
        return true, nil
    }
    return false, nil
})
```

_See [crd_checks.go](./crd_checks.go)._

### Verify resource state transitions

To use the [polling functions](#poll-knative-serving-resources) you must provide a function to check the
state. Some of the expected transition states (as defined in [the Knative Serving spec](/docs/spec/spec.md))
are expressed in functions in [states.go](./states.go).

For example when a `Revision` has been created, the system will start the resources required to
actually serve it, and then the `Revision` object will be updated to indicate it is ready. This
can be polled with `test.IsRevisionReady`:

```go
err := test.WaitForRevisionState(clients.Revisions, revisionName, test.IsRevisionReady(revisionName))
if err != nil {
    t.Fatalf("Revision %s did not become ready to serve traffic: %v", revisionName, err)
}
```

Once the `Revision` is created, all traffic for a `Route` should be routed to it. This can be polled with
`test.AllRouteTrafficAtRevision`:

```go
err = test.WaitForRouteState(clients.Routes, routeName, test.AllRouteTrafficAtRevision(routeName, revisionName))
if err != nil {
    t.Fatalf("The Route %s was not updated to route traffic to the Revision %s: %v", routeName, revisionName, err)
}
```

_See [states.go](./states.go)._

### Generate boilerplate CRDs

Your tests will probably need to create `Route` and `Configuration` objects. You can use the
existing boilerplate to describe them.

You can also use the function `RandomizedName` to create a random name for your `crd` so that
your tests can use unique names each time they run.

For example to create a `Configuration` object that uses a certain docker image with a
randomized name:

```go
var names test.ResourceNames
names.Config := test.RandomizedName('hotdog')
_, err := clients.Configs.Create(test.Configuration(namespaceName, names, imagePath))
if err != nil {
    return err
}
```

Please expand these functions as more use cases are tested.

_See [crd.go](./crd.go)._

### Ensure test cleanup

To ensure your test is cleaned up, you should defer cleanup to execute after your
test completes and also ensure the cleanup occurs if the test is interrupted:

```go
defer tearDown(clients)
test.CleanupOnInterrupt(func() { tearDown(clients) })
```

_See [cleanup.go](./cleanup.go)._
