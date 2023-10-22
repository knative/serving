This is the instruction about how to run External Domain TLS E2E test under different
configurations to test different use cases. For more details about External Domain TLS
feature, check out the [External Domain TLS](https://knative.dev/docs/serving/using-external-domain-tls/)
feature documentation.

# Prerequisites
* Have `cert-manager` installed
* Have `net-certmanager` installed
* Upload test images with `./test/upload-test-images.sh`
* Enable `external-domain-tls` with `kubectl patch cm config-network -n knative-serving -p '{"data":{"external-domain-tls": "enabled"}}'`

To run External Domain TLS E2E test locally, run the following commands:

1. test case 1: testing per ksvc certificate provision with self-signed CA
   1. Run `kubectl patch cm config-network -n knative-serving -p '{"data":{"namespace-wildcard-cert-selector": ""}}'` to disable wildcards for namespaces
   1. `kubectl delete kcert --all -n serving-tests`
   1. `kubectl apply -f test/config/externaldomaintls/certmanager/selfsigned/`
   1. `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/externaldomaintls/... -run ^TestTLS`
1. test case 2: testing per namespace certificate provision with self-signed CA
   1. `kubectl delete kcert --all -n serving-tests`
   1. `kubectl apply -f test/config/externaldomaintls/certmanager/selfsigned/`
   1. Run `kubectl patch cm config-network -n knative-serving -p '{"data":{"namespace-wildcard-cert-selector": "{}"}}'` to enable wildcards for all namespaces
   1. `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/externaldomaintls/... -run ^TestTLS`
1. test case 3: testing per ksvc certificate provision with HTTP challenge
   1. Run `kubectl patch cm config-network -n knative-serving -p '{"data":{"namespace-wildcard-cert-selector": ""}}'` to disable wildcards for namespaces
   1. `kubectl delete kcert --all -n serving-tests`
   1. `kubectl apply -f test/config/externaldomaintls/certmanager/http01/`
   1. `export SERVICE_NAME=http01`
   1. `kubectl patch cm config-domain -n knative-serving -p '{"data":{"<your-custom-domain>":""}}'`
   1. Add a DNS A record to map host `http01.serving-tests.<your-custom-domain>`
      to the Ingress IP.
   1. `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/externaldomaintls/... -run ^TestTLS`
