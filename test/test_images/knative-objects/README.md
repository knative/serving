# Knative-Objects test image

This directory contains the test image used for testing knative object actions for 
knative performance testing

The image contains a simple Go webserver, `knative_objects.go`, that exposes different
endpoints that perform the said actions. The currently supported actions are:

|     Action     |     Endpoint    |
|:--------------:|:---------------:|
| create service | /create_service |

## Building image

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).

## Using image

The image is auto uploaded to the repository when running the performance tests. To use the image:

1. Create a resourcenames object with the `knative-objects` image name

```go
names := test.ResourceNames{
	Service: test.AppendRandomString(testName, logger),
	Image:   "knative-objects",
}
```

2. Create a new service using `CreateRunLatestServiceReady` passing that object. This creates and returns the necessary knative objects. It also and exposes the app endpoints
that perform operations on knative objects.

```go
objs, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{})
```

3. Get the domain returned in the knative route object.

```go
domain := objs.Route.Status.Domain
```

4. Get the endpoint from the create service
```go
endpoint, err := spoof.GetServiceEndpoint(clients.KubeClient.Kube)
```

5. Use the load generator to perform [load testing](https://github.com/knative/serving/blob/master/test/performance/README.md) 
any exposed endpoint