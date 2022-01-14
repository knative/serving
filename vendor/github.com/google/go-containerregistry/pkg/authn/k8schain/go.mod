module github.com/google/go-containerregistry/pkg/authn/k8schain

go 1.14

require (
	github.com/awslabs/amazon-ecr-credential-helper/ecr-login v0.0.0-20211027214941-f15886b5ccdc
	github.com/chrismellard/docker-credential-acr-env v0.0.0-20210203204924-09e2b5a8ac86
	github.com/google/go-containerregistry v0.8.1-0.20220110151055-a61fd0a8e2bb
	github.com/google/go-containerregistry/pkg/authn/kubernetes v0.0.0-20220110151055-a61fd0a8e2bb
	k8s.io/api v0.22.5
	k8s.io/client-go v0.22.5
)

replace (
	github.com/google/go-containerregistry => ../../../
	github.com/google/go-containerregistry/pkg/authn/kubernetes => ../kubernetes/
)
