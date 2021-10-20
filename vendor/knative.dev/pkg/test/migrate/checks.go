package migrate

/*
Copyright 2021 The Knative Authors

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

import (
	"context"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExpectSingleStoredVersion verifies that status.storedVersions on specific CRDs has only one version
// and the version is listed in spec.Versions with storage: true. It means the CRDs
// have been migrated and previous/unused API versions can be safely removed from the spec.
func ExpectSingleStoredVersion(t *testing.T, crdClient apiextensionsv1.CustomResourceDefinitionInterface, crdGroup string) {
	t.Helper()

	crdList, err := crdClient.List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal("Unable to fetch crd list:", err)
	}

	for _, crd := range crdList.Items {
		if strings.Contains(crd.Name, crdGroup) {
			if len(crd.Status.StoredVersions) != 1 {
				t.Errorf("%q does not have a single stored version: %+v", crd.Name, crd)
			}
			stored := crd.Status.StoredVersions[0]
			for _, v := range crd.Spec.Versions {
				if stored == v.Name && !v.Storage {
					t.Errorf("%q is invalid: spec.versions.storage must be true for %q or "+
						"version %q must be removed from status.storageVersions: %+v", crd.Name, v.Name, v.Name, crd)
				}
			}
		}
	}
}
