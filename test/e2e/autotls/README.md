This is the instruction about how to run Auto TLS E2E test under different
configurations to test different use cases. For more details about Auto TLS
feature, check out the
[Auto TLS](https://knative.dev/docs/serving/using-auto-tls/) feature
documentation.

To run Auto TLS E2E test locally, run the following commands:

1. test case 1: testing per ksvc certificate provision with self-signed CA
   1. `kubectl label namespace serving-tests networking.internal.knative.dev/disableWildcardCert=true`
   1. `kubectl delete kcert --all -n serving-tests`
   1. `kubectl apply -f test/config/autotls/certmanager/selfsigned/`
   1. `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/autotls/... -run ^TestTls`
1. test case 2: testing per namespace certificate provision with self-signed CA
   1. `kubectl delete kcert --all -n serving-tests`
   1. `kubectl apply -f test/config/autotls/certmanager/selfsigned/`
   1. Run `kubectl edit namespace serving-tests` and remove the label
      networking.internal.knative.dev/disableWildcardCert
   1. `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/autotls/... -run ^TestTls`
1. test case 3: testing per ksvc certificate provision with HTTP challenge
   1. `kubectl label namespace serving-tests networking.internal.knative.dev/disableWildcardCert=true`
   1. `kubectl delete kcert --all -n serving-tests`
   1. `kubectl apply -f test/config/autotls/certmanager/http01/`
   1. `export SERVICE_NAME=http01`
   1. `kubectl patch cm config-domain -n knative-serving -p '{"data":{"<your-custom-domain>":""}}'`
   1. Add a DNS A record to map host `http01.serving-tests.<your-custom-domain>`
      to the Ingress IP.
   1. `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/autotls/... -run ^TestTls`
