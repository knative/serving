module knative.dev/serving

go 1.14

require (
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/cli v0.0.0-20200210162036-a4bedce16568 // indirect
	github.com/ghodss/yaml v1.0.0
	github.com/golang/protobuf v1.3.5
	github.com/google/go-cmp v0.4.0
	github.com/google/go-containerregistry v0.0.0-20200331213917-3d03ed9b1ca2
	github.com/google/gofuzz v1.1.0
	github.com/google/mako v0.0.0-20190821191249-122f8dcef9e3
	github.com/google/uuid v1.1.1
	github.com/gorilla/websocket v1.4.0
	github.com/hashicorp/golang-lru v0.5.4
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/kubernetes-incubator/custom-metrics-apiserver v0.0.0-20190918110929-3d9be26a50eb
	github.com/mattbaird/jsonpatch v0.0.0-20171005235357-81af80346b1a
	github.com/prometheus/client_golang v1.5.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.9.1
	github.com/spf13/pflag v1.0.5
	github.com/tsenart/vegeta v12.7.1-0.20190725001342-b5f4fca92137+incompatible
	go.opencensus.io v0.22.3
	go.uber.org/atomic v1.6.0
	go.uber.org/zap v1.14.1
	golang.org/x/net v0.0.0-20200324143707-d3edc9973b7e
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0
	google.golang.org/api v0.20.0
	google.golang.org/grpc v1.28.1
	istio.io/api v0.0.0-20200512234804-e5412c253ffe
	istio.io/client-go v0.0.0-20200513000250-b1d6e9886b7b
	istio.io/gogo-genproto v0.0.0-20191029161641-f7d19ec0141d // indirect
	k8s.io/api v0.18.1
	k8s.io/apimachinery v0.18.1
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/code-generator v0.18.0
	k8s.io/kube-openapi v0.0.0-20200410145947-bcb3869e6f29
	k8s.io/metrics v0.17.6
	knative.dev/caching v0.0.0-20200606210318-787aec80f71c
	knative.dev/networking v0.0.0-20200607161819-2086ac6759c2
	knative.dev/pkg v0.0.0-20200606224418-7ed1d4a552bc
	knative.dev/test-infra v0.0.0-20200606045118-14ebc4a42974
)

// pin the older grpc - see: https://github.com/grpc/grpc-go/issues/3180
// etcd go lib hasn't upgraded yet (etcd lib comes from custom metrics server)
//
// a side effect is we need to pin other deps to when they used the older grpc version
replace (
	cloud.google.com/go => cloud.google.com/go v0.52.0
	google.golang.org/api => google.golang.org/api v0.15.1
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20200115191322-ca5a22157cba
	google.golang.org/grpc => google.golang.org/grpc v1.26.0
)

replace (
	github.com/Azure/azure-sdk-for-go => github.com/Azure/azure-sdk-for-go v38.2.0+incompatible
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.4.0+incompatible
	github.com/coreos/etcd => github.com/coreos/etcd v3.3.13+incompatible

	github.com/kubernetes-incubator/custom-metrics-apiserver => github.com/kubernetes-incubator/custom-metrics-apiserver v0.0.0-20200323093244-5046ce1afe6b

	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2

	github.com/tsenart/vegeta => github.com/tsenart/vegeta v1.2.1-0.20190917092155-ab06ddb56e2f

	k8s.io/api => k8s.io/api v0.17.6
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.17.6
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.6
	k8s.io/apiserver => k8s.io/apiserver v0.17.6
	k8s.io/client-go => k8s.io/client-go v0.17.6
	k8s.io/code-generator => k8s.io/code-generator v0.17.6
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20200410145947-bcb3869e6f29
	k8s.io/metrics => k8s.io/metrics v0.17.6

	knative.dev/serving/vendor/k8s.io/code-generator/vendor/github.com/spf13/pflag => github.com/spf13/pflag v1.0.5
)
