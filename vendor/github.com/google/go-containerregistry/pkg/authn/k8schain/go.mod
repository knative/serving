module github.com/google/go-containerregistry/pkg/authn/k8schain

go 1.14

require (
	github.com/google/go-containerregistry v0.5.2-0.20210609162550-f0ce2270b3b4
	github.com/vdemeester/k8s-pkg-credentialprovider v1.21.0-1
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d // indirect
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
)

replace github.com/google/go-containerregistry => ../../..
