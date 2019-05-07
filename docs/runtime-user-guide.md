# Runtime User Guide

This document is focused on clarifying the expectations of the user
container running in a Knative envioronment. This is separate from the expectations of the
environment itself. The expectations of the enviornment are detailed in the [runtime
contract](https://github.com/knative/serving/blob/master/docs/runtime-contract.md).

The target audience of this document are _developers_ and _language and tooling
developers_ as defined below:

- **Developers** write code which is packaged into a container which is run on
  the Knative cluster.
- **Language and tooling developers** typically write tools used by
    _developers_ to package code into containers. As such, they are concerned
    that tooling which wraps developer code complies with this runtime contract.

## General Behavior

Containers running in Knative should be stateless containers. Stateless
containers should have the following properties:

- Fast startup time: The time from container launch to serving traffic should be
  minmized to enable the platform to provide rapid autoscaling and scale-to-zero
  semantics.
- Minimize local state: The container may be killed and replaced to support
  autoscaling and cluster maintence operations. The performance or correctness
  of the application should not rely on state persisted locally.
- CPU usage only while requests are active: As containers may be killed or
  suspended when no connections are active the container should only perform
  operations that complete synchronously with the request's response.

## Container Termination

Knative containers should respond to `SIGTERM` signals to clean-up any resources
necessary for a graceful shutdown.

## Connection

A Knative container must package a webserver that can respond to HTTP/1.1
requests on a specified port.

All traffic the comes to the container should be expected to pass through a
proxy.

### Ports

All Knative containers must listen for incoming traffic on a specific port. A
port may be specified through the API otherwise a platform-provided port will be chosen.

The selected port will be made available within the container as the enviornment variable `$PORT`.
It is recommended to consume the enviornment variable `$PORT` rather than hard-code a particular
port within the application.

Example (See
[Container](https://github.com/knative/serving/blob/master/docs/spec/spec.md#container))
```yaml
...
  containers:
   - image: example-image:1.0
     ports:
      - containerPort: 8081
...
```

### Protocols

HTTP/1.1 is the default protocol for Knative. To use HTTP/2.0 you must specify
`h2c` in the API using the port `name` field.

Example (See
[Container](https://github.com/knative/serving/blob/master/docs/spec/spec.md#container))
```yaml
...
  containers:
   - image: example-image:1.0
     ports:
      - name: "h2c"
        containerPort: 8081
...
```

## Storage

Container storage may remain across requests, but no local storage should be used for
persistent data as it is not retained in the event of container termination.

It is recommended to store temporary state such as local caches or working data in `/tmp`
as this space is guarenteed to be present and mounted Read-Write.

# Probes

By default, the readiness of a Knative container is determined by a successful TCP
response on the specified port for incoming traffic. If this readiness check is not
sufficient to determine the readiness of the container to serve traffic, then an
additional check should be specified through the `readinessProbes` section of
the API.

Example (See
[Container](https://github.com/knative/serving/blob/master/docs/spec/spec.md#container))

```yaml
...
  containers:
   - image: example-image:1.0
     readinessProbe:
       httpGet:
         path: /health
         periodSeconds: 10
         initialDelaySeconds: 0
...
```

It is recommended to set the `initialDelaySeconds` of any readiness or liveness
check to 0 as any higher values set a floor on container cold-start performance.

# Logging

Log statements should be sent to `stdout` and `stderr`. Log statements sent to
these locations will be collected by the underlying platform and made visible.
