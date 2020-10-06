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
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
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
func newResolverTransport(path string, maxIdleConns, maxIdleConnsPerHost int) (*http.Transport, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}

	if crt, err := ioutil.ReadFile(path); err != nil {
		return nil, err
	} else if ok := pool.AppendCertsFromPEM(crt); !ok {
		return nil, errors.New("failed to append k8s cert bundle to cert pool")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = maxIdleConns
	transport.MaxIdleConnsPerHost = maxIdleConnsPerHost
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}

	return transport, nil
}

// Resolve resolves the image references that use tags to digests.
func (r *digestResolver) Resolve(
	ctx context.Context,
	image string,
	opt k8schain.Options,
	registriesToSkip sets.String) (string, error) {
	kc, err := k8schain.New(ctx, r.client, opt)
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

	desc, err := remote.Head(tag, remote.WithContext(ctx), remote.WithTransport(r.transport), remote.WithAuthFromKeychain(kc))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s@%s", tag.Repository.String(), desc.Digest), nil
}
