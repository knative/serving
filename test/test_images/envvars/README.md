# Envvars test image

This directory contains the test image used in the envvars e2e test.

The image contains a simple Go webserver, `envvars.go`, that will, by default, listen on port `8080` and expose call to paths /envvars/must and /envvars/should `.

'must' and 'should' refer to the keywords "MUST" and "SHOULD" used in the Knative [run-time contract](/docs/runtime-contract.md) and provides environment variables that "MUST" and "SHOULD" be set by a Knative implementations

When called, the server emits JSON response containing the values of the environment variables for the corresponding path

## Building

For details about building and adding new images, see the [section about test
images](/test/README.md#test-images).

