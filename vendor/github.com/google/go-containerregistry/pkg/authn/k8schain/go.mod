module github.com/google/go-containerregistry/pkg/authn/k8schain

go 1.14

require (
	github.com/google/go-containerregistry v0.5.2-0.20210609162550-f0ce2270b3b4
	github.com/vdemeester/k8s-pkg-credentialprovider v1.20.7
	k8s.io/api v0.20.7
	k8s.io/apimachinery v0.20.7
	k8s.io/client-go v0.20.7
)

replace github.com/google/go-containerregistry => ../../..
