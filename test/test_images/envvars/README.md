# Helloworld test image

This directory contains the test image used in the envvars e2e test.

The image contains a simple Go webserver, `envvars.go`, that will, by default, listen on port `8080` and expose a service at `/`.

When called, the server emits a message containing the values of the environment variables for knative service, configuraiton and revision.

## Building

For details about building and adding new images, see the [section about test
images](/test/README.md#test-images).

