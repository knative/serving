# Internal Encryption E2E Tests

In order to test Internal Encryption, this test looks at the `security_mode` tag on `request_count` metrics from the Activator and QueueProxy.

The `metricsreader` test image was created for this purpose. Given the PodIPs of the Activator and the Knative Service pod (i.e. the QueueProxy), it will make requests to each respective `/metrics` endpoint, pull out the `*_request_count` metric, look for the tag `security_mode`, and respond with a map of tag values to counts. The [README.md](../../test_images/metricsreader/README.md) will explain in more detail.

The test works as follows:
* The [setup script](../../e2e-internal-encryption-tests.sh) configures the `dataplane-trust` config in `config-network` to `enabled`.
* The [test](internalencryption_test.go) deploys the `metricsreader` Knative Service. This service uses annotations to set the initial, min, and max scale to 1. This is to guarantee the PodIP is consistent during the test, and avoid complications of having multiple instances.
* The test then extracts the PodIPs of the activator and the pod of the latest `metricsreader` Revision.
* The test will make 3 requests to the `metricsreader` pod:
  * A GET request to make sure it is alive.
  * A first POST request to get the initial `request_count`s.
  * A second POST request to get the updated `request_count`s.
* The test will then compare the counts between the initial and updated requests to determine if the counts have increased, indicating that Internal Encryption is indeed happening.
