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
	"fmt"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"k8s.io/client-go/kubernetes"
)

type digestResolver struct {
	client    kubernetes.Interface
	transport http.RoundTripper
}

// Resolve resolves the image references that use tags to digests.
func (r *digestResolver) Resolve(
	image string,
	opt k8schain.Options,
	registriesToSkip map[string]struct{},
) (string, error) {
	kc, err := k8schain.New(r.client, opt)
	if err != nil {
		return "", err
	}

	if _, err := name.NewDigest(image, name.WeakValidation); err == nil {
		// Already a digest
		return image, nil
	}

	tag, err := name.NewTag(image, name.WeakValidation)
	if err != nil {
		return "", err
	}

	if _, ok := registriesToSkip[tag.Registry.RegistryStr()]; ok {
		return "", nil
	}

	img, err := remote.Image(tag, remote.WithTransport(r.transport), remote.WithAuthFromKeychain(kc))
	if err != nil {
		return "", err
	}
	digest, err := img.Digest()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s@%s", tag.Repository.String(), digest), nil
}
