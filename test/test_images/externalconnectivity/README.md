# Externalconnectivity test image

This directory contains the test image used in the external connectivity e2e test.

The image contains a simple Go webserver, `externalconnectivity.go`, that will, by
default, listen on port `8080` and expose a service at `/`.

When called, the server will curl an external address, and emit a "success" message if successful.

## Trying out

To run the image as a Service outisde of the test suite:

`ko apply -f service.yaml`

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
