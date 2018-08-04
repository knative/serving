# Helloworld test image

This directory contains the test image used in the helloworld e2e test.

The image contains a simple Go webserver, `helloworld.go`, that will, by default, listen on port `8080` and expose a service at `/`.

When called, the server emits a "hello world" message.

## Building

For details about building and adding new images, see the [section about test
images](/test/README.md#test-images).

