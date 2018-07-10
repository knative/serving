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

	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1/remote"
	"github.com/mattmoor/k8schain"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
)

type digestResolver struct {
	client           kubernetes.Interface
	transport        http.RoundTripper
	registriesToSkip map[string]struct{}
}

// Resolve resolves the image references that use tags to digests.
func (r *digestResolver) Resolve(deploy *appsv1.Deployment) error {
	pod := deploy.Spec.Template.Spec
	opt := k8schain.Options{
		Namespace:          deploy.Namespace,
		ServiceAccountName: pod.ServiceAccountName,
		// ImagePullSecrets: Not possible via RevisionSpec, since we
		// don't expose such a field.
	}
	kc, err := k8schain.New(r.client, opt)
	if err != nil {
		return err
	}

	for i := range pod.Containers {
		if _, err := name.NewDigest(pod.Containers[i].Image, name.WeakValidation); err == nil {
			// Already a digest
			continue
		}
		tag, err := name.NewTag(pod.Containers[i].Image, name.WeakValidation)
		if err != nil {
			return err
		}

		if _, ok := r.registriesToSkip[tag.Registry.RegistryStr()]; ok {
			continue
		}

		auth, err := kc.Resolve(tag.Registry)
		if err != nil {
			return err
		}
		img, err := remote.Image(tag, auth, r.transport)
		if err != nil {
			return err
		}
		digest, err := img.Digest()
		if err != nil {
			return err
		}
		pod.Containers[i].Image = fmt.Sprintf("%s@%s", tag.Repository.String(), digest)
	}
	return nil
}
