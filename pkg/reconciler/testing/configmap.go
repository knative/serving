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

package testing

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

// ConfigMapFromTestFile creates a v1.ConfigMap from a YAML file
// It loads the YAML file from the testdata folder.
func ConfigMapFromTestFile(t *testing.T, name string, allowed ...string) *corev1.ConfigMap {
	t.Helper()

	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s.yaml", name))
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}

	var cm corev1.ConfigMap

	// Use github.com/ghodss/yaml since it reads json struct
	// tags so things unmarshal properly
	if err := yaml.Unmarshal(b, &cm); err != nil {
		t.Fatalf("yaml.Unmarshal() = %v", err)
	}

	if len(cm.Data) != len(allowed) {
		// See here for why we only check in empty ConfigMaps:
		// https://github.com/knative/serving/issues/2668
		t.Errorf("Data = %v, wanted allowed", cm.Data)
	}
	allow := sets.NewString(allowed...)
	for key := range cm.Data {
		if !allow.Has(key) {
			t.Errorf("Encountered key %q in %q that wasn't on the allowed list", key, name)
		}
	}

	// If the ConfigMap has no data entries, then make sure we don't have
	// a `data:` key, or it will undo the whole motivation for leaving the
	// data empty: https://github.com/knative/serving/issues/2668
	if len(cm.Data) == 0 {
		var u unstructured.Unstructured
		if err := yaml.Unmarshal(b, &u); err != nil {
			t.Errorf("yaml.Unmarshal(%q) = %v", name, err)
		}
		if _, ok := u.Object["data"]; ok {
			t.Errorf("%q should omit its empty `data:` section.", name)
		}
	}

	return &cm
}
