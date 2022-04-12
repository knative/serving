module knative.dev/serving

go 1.16

require (
	github.com/ahmetb/gen-crd-api-reference-docs v0.3.1-0.20210609063737-0067dc6dcea2
	github.com/c2h5oh/datasize v0.0.0-20200112174442-28bbd4740fee // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.11.0 // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.7
	github.com/google/go-containerregistry v0.8.1-0.20220110151055-a61fd0a8e2bb
	github.com/google/go-containerregistry/pkg/authn/k8schain v0.0.0-20220328141311-efc62d802606
	github.com/google/gofuzz v1.2.0
	github.com/gorilla/websocket v1.4.2
	github.com/hashicorp/golang-lru v0.5.4
	github.com/influxdata/tdigest v0.0.1 // indirect
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/prometheus/client_golang v1.11.1
	github.com/prometheus/client_model v0.2.0
	github.com/tsenart/go-tsz v0.0.0-20180814235614-0bd30b3df1c3 // indirect
	github.com/tsenart/vegeta/v12 v12.8.4
	go.opencensus.io v0.23.0
	go.uber.org/atomic v1.9.0
	go.uber.org/automaxprocs v1.4.0
	go.uber.org/zap v1.19.1
	golang.org/x/net v0.0.0-20220225172249-27dd8689420f
	golang.org/x/oauth2 v0.0.0-20220223155221-ee480838109b
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/time v0.0.0-20220224211638-0e9765cccd65
	golang.org/x/tools v0.1.9 // indirect
	google.golang.org/api v0.70.0
	google.golang.org/grpc v1.44.0
	k8s.io/api v0.23.5
	k8s.io/apiextensions-apiserver v0.23.4
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
	k8s.io/code-generator v0.23.5
	k8s.io/klog v1.0.0 // indirect
	k8s.io/kube-openapi v0.0.0-20220124234850-424119656bbf
	knative.dev/caching v0.0.0-20220412015107-318083f279be
	knative.dev/hack v0.0.0-20220411131823-6ffd8417de7c
	knative.dev/networking v0.0.0-20220412073408-56c4a3e1329c
	knative.dev/pkg v0.0.0-20220411234407-00c122e376d0
	sigs.k8s.io/yaml v1.3.0
)
