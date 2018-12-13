# Environment test image

This directory contains the test image used to retrieve environment information
under which the container runs. This is used by conformance tests to verify
Knative [run-time contract](/docs/runtime-contract.md)

The image contains a simple Go webserver, `environment.go`, which by default,
listens on port defined in the constant
[EnvImageServerPort](/test/conformance/constants.go).

Currently the server exposes:

- /envvars : To provide a JSON payload containing all the environment variables
  set inside the container

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
