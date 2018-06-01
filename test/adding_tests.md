# Adding tests

If you are [developing knative](/DEVELOPMENT.md) you may need to add or change:

* [e2e tests](./e2e)
* [Conformance tests](./conformance)

Both tests can use our [test library](#test-library).

Reviewers of conformance and e2e tests (i.e. [OWNERS](/test/OWNERS)) are responsible for the style and quality of the resulting tests. In order to not discourage contributions, when style change are required, the reviewers can make the changes themselves.

## Presubmit tests

[`presubmit-tests.sh`](./presubmit-tests.sh) is the entry point for both the [end-to-end tests](/test/e2e) and the [conformance tests](/test/conformance)

This script, and consequently, the e2e and conformance tests will be run before every code submission. You can run these tests manually with:

```shell
test/presubmit-tests.sh
```

## Test library

In the [`test`](/test/) dir you will find several libraries in the `test` package
you can use in your tests.

You can:
* [Use common test flags](#use-common-test-flags)
* [Get access to client objects](#get-access-to-client-objects)
* [Make requests against deployed services](#make-requests-against-deployed-services)
* [Poll Knative Serving resources](#poll-knative-serving-resources)
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

### Poll Knative Serving resources

After creating Knative Serving resources or making changes to them, you will need to wait for the system
to realize those chnages. You can use the Knative Serving CRD polling methods to poll the resources until
they get into the desired state or time out.

These functions use the kubernetes [`wait` package](https://godoc.org/k8s.io/apimachinery/pkg/util/wait).
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

_See [crd_polling.go](./crd_polling.go)._

### Verify resource state transitions

To use the [polling functions](#poll-knative-resources) you must provide a function to check the
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

For example to create a `Configuration` object that uses a certain docker image:

```go
_, err := clients.Configs.Create(test.Configuration(namespaceName, configName, imagePath))
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
