# ObservedConcurrency test image

This directory contains the test image used in the observed concurrency performance test.

The image contains a simple Go webserver, `observed_concurrency.go`, that sets a single concurrency model for the service.

Each request will return its serverside start and end-time in nanoseconds.

## Building

For details about building and adding new images, see the [section about test
images](/test/README.md#test-images).