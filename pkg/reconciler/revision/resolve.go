/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package revision

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
)

type digestResolver struct {
	client    kubernetes.Interface
	transport http.RoundTripper
}

const (
	// Kubernetes CA certificate bundle is mounted into the pod here, see:
	// https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster/#trusting-tls-in-a-cluster
	k8sCertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// newResolverTransport returns an http.Transport that appends the certs bundle
// at path to the system cert pool.
//
// Use this with k8sCertPath to trust the same certs as the cluster.
func newResolverTransport(path string) (*http.Transport, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}

	if crt, err := ioutil.ReadFile(path); err != nil {
		return nil, err
	} else if ok := pool.AppendCertsFromPEM(crt); !ok {
		return nil, errors.New("failed to append k8s cert bundle to cert pool")
	}

	// Copied from https://github.com/golang/go/blob/release-branch.go1.12/src/net/http/transport.go#L42-L53
	// We want to use the DefaultTransport but change its TLSClientConfig. There
	// isn't a clean way to do this yet: https://github.com/golang/go/issues/26013
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// Use the cert pool with k8s cert bundle appended.
		TLSClientConfig: &tls.Config{
			RootCAs: pool,
		},
	}, nil
}

// Resolve resolves the image references that use tags to digests.
func (r *digestResolver) Resolve(
	image string,
	opt k8schain.Options,
	registriesToSkip sets.String) (string, error) {
	kc, err := k8schain.New(r.client, opt)
	if err != nil {
		return "", fmt.Errorf("failed to initialize authentication: %w", err)
	}

	if _, err := name.NewDigest(image, name.WeakValidation); err == nil {
		// Already a digest
		return image, nil
	}

	tag, err := name.NewTag(image, name.WeakValidation)
	if err != nil {
		return "", fmt.Errorf("failed to parse image name %q into a tag: %w", image, err)
	}

	if registriesToSkip.Has(tag.Registry.RegistryStr()) {
		return "", nil
	}
	platform := v1.Platform{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
	}
	desc, err := remote.Get(tag, remote.WithTransport(r.transport), remote.WithAuthFromKeychain(kc), remote.WithPlatform(platform))
	if err != nil {
		return "", fmt.Errorf("failed to fetch image information: %w", err)
	}

	// TODO(#3997): Use remote.Get to resolve manifest lists to digests as well
	// once CRI-O is fixed: https://github.com/cri-o/cri-o/issues/2157
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		img, err := desc.Image()
		if err != nil {
			return "", fmt.Errorf("failed to get image reference: %w", err)
		}
		dgst, err := img.Digest()
		if err != nil {
			return "", fmt.Errorf("failed to get image digest: %w", err)
		}
		return fmt.Sprintf("%s@%s", tag.Repository.String(), dgst), nil
	default:
		return fmt.Sprintf("%s@%s", tag.Repository.String(), desc.Digest), nil
	}
}
