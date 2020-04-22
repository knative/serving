# Test

This directory contains Ingress conformance tests for Knative Ingress resource.

## Environment requirements

### Development tools

1. [`go`](https://golang.org/doc/install): The language `Knative Serving` is
   built in (1.13 or later)
1. [`ko`](https://github.com/google/ko): Build tool to setup the environment.
1. [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/): For
   managing development environments.

### Test environment

1. [A running `Knative Serving` cluster.](../../../DEVELOPMENT.md#prerequisites),
   with the Ingress implementation of choice installed.
   ```bash
   # Set the Ingress class annotation to use in tests.
   # Some examples:
   #   export INGRESS_CLASS=gloo.ingress.networking.knative.dev      # Gloo Ingress
   #   export INGRESS_CLASS=istio.ingress.networking.knative.dev     # Istio Ingress
   #   export INGRESS_CLASS=kourier.ingress.networking.knative.dev   # Kourier Ingress
   export INGRESS_CLASS=<your-ingress-class-annotation>
   ```
1. Knative Serving source code check out at `${SERVING_ROOT}`. Often this is
   `$GO_PATH/src/go/knative.dev/serving`. This contains all the tests and the
   test images.
   ```bash
   export SERVING_ROOT=<where-you-checked-out-knative/serving>
   ```
1. A docker repo containing [the test images](#test-images) `KO_DOCKER_REPO`:
   The docker repository to which developer images should be pushed (e.g.
   `gcr.io/[gcloud-project]`).

   ```bash
   export KO_DOCKER_REPO=<your-docker-repository>
   ```
1. The `knative-testing` resources

   ```bash
   ko apply -f "$SERVING_ROOT/test/config"
   ```

## Building the test images

Note: this is only required when you run conformance/e2e tests locally with
`go test` commands.

The [`upload-test-images.sh`](../../upload-test-images.sh) script can be used to
build and push the test images used by the conformance and e2e tests. The script
expects your environment to be setup as described in
[DEVELOPMENT.md](../../../DEVELOPMENT.md#install-requirements).

To run the script for all end to end test images:

```bash
$SERVING_ROOT/test/upload-test-images.sh
```

## Running the tests

```bash
cd "$SERVING_ROOT"

# Set the endpoint of your Ingress installation.
#
# For example:
#    export INGRESS_ENDPOINT="$(minikube ip):31380"
export INGRESS_ENDPOINT=<your-ingress-ip/url>:<your-ingress-port>

go test -v -tags=e2e -count=1 test/conformance/ingress \
    --ingressClass="$INGRESS_CLASS" \
    --ingressendpoint=$INGRESSENDPOINT
```
