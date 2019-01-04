# Timeout test image

This directory contains the test image used in the destroy pod e2e test.

The image contains a simple Go webserver, `timeout.go`, that will, by default,
listen on port `8080` and expose a service at `/`.

When called, the server sleeps for the amount of milliseconds passed in via the
query parameter `timeout` and responds with "Slept for X milliseconds` where X
is the amount of milliseconds passed in.

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
