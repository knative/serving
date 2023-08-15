# Metricsreader test image

This directory contains the test image used in the Interal Encryption e2e test.

The image contains a simple Go webserver, `metricsreader.go`, that will, by
default, listen on port `8080` and expose a service at `/`.

A `GET` request just returns a simple hello message.

A `POST` request with the IP addresses of the Activator and the Pod for the latest revision will prompt the server to make requests to the metrics endpoints on each IP, and collect the `*_request_count` metrics. It will check for the tag `security_mode` on each metric, and report the counts based on the value of the tag. The `security_mode` tag corresponds to the possible values for the config option `dataplane-trust` in `config-network`.

This is used in the test to make sure that, when Internal Encryption is enabled (`dataplane-trust != Disabled`), the Activator and Queue Proxies are handling TLS connections.

An example request and response looks like this:
```
❯ curl -X POST http://metricsreader-test-image.default.kauz.tanzu.biz -H "Content-Type: application/json" -d '{"activator_ip": "10.24.1.132", "queue_ip": "10.24.3.164"}' | jq .

{
  "ActivatorData": {
    "disabled": 0,
    "enabled": 0,
    "identity": 0,
    "minimal": 1,
    "mutual": 0
  },
  "QueueData": {
    "disabled": 0,
    "enabled": 0,
    "identity": 0,
    "minimal": 1,
    "mutual": 0
  }
}
```

By default, the Knative Service is set with an initial-scale, min-scale, and max-scale of 1. This is to make it possible to know the IP of the Queue Proxy before making the call, and to avoid any complications due to multiple instances.

## Trying out

To run the image as a Service outside of the test suite:

`ko apply -f service.yaml`

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
