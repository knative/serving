module knative.dev/serving

go 1.16

require (
	github.com/ahmetb/gen-crd-api-reference-docs v0.3.1-0.20210609063737-0067dc6dcea2
	github.com/c2h5oh/datasize v0.0.0-20200112174442-28bbd4740fee // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/go-logr/zapr v1.2.2
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.6
	github.com/google/go-containerregistry v0.7.1-0.20211118220127-abdc633f8305
	github.com/google/go-containerregistry/pkg/authn/k8schain v0.0.0-20211118220127-abdc633f8305
	github.com/google/gofuzz v1.2.0
	github.com/gorilla/websocket v1.4.2
	github.com/hashicorp/golang-lru v0.5.4
	github.com/influxdata/tdigest v0.0.1 // indirect
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/miekg/dns v1.1.29 // indirect
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/client_model v0.2.0
	github.com/tsenart/go-tsz v0.0.0-20180814235614-0bd30b3df1c3 // indirect
	github.com/tsenart/vegeta/v12 v12.8.4
	go.opencensus.io v0.23.0
	go.uber.org/atomic v1.9.0
	go.uber.org/automaxprocs v1.4.0
	go.uber.org/zap v1.19.1
	golang.org/x/net v0.0.0-20211209124913-491a49abca63
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/time v0.0.0-20211116232009-f0f3c7e86c11
	google.golang.org/api v0.61.0
	google.golang.org/grpc v1.42.0
	k8s.io/api v0.22.5
	k8s.io/apiextensions-apiserver v0.22.5
	k8s.io/apimachinery v0.22.5
	k8s.io/client-go v0.22.5
	k8s.io/code-generator v0.22.5
	k8s.io/kube-openapi v0.0.0-20211109043538-20434351676c
	knative.dev/caching v0.0.0-20220113145613-9df2c0c8a931
	knative.dev/hack v0.0.0-20220111151514-59b0cf17578e
	knative.dev/networking v0.0.0-20220112013650-eac673fb5c49
	knative.dev/pkg v0.0.0-20220113045912-c0e1594c2fb1
	sigs.k8s.io/yaml v1.3.0
)
