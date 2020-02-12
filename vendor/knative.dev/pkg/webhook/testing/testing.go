/*
Copyright 2019 The Knative Authors

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
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	jsonpatch "gomodules.xyz/jsonpatch/v2"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/system"
	pkgtest "knative.dev/pkg/testing"

	// Makes system.Namespace work in tests.
	_ "knative.dev/pkg/system/testing"
)

// CreateResource creates a testing.Resource with the given name in the system namespace.
func CreateResource(name string) *pkgtest.Resource {
	return &pkgtest.Resource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      name,
		},
		Spec: pkgtest.ResourceSpec{
			FieldWithValidation: "magic value",
		},
	}
}

// ExpectAllowed checks that a given admission response allows the initiating request through.
func ExpectAllowed(t *testing.T, resp *admissionv1beta1.AdmissionResponse) {
	t.Helper()
	if !resp.Allowed {
		t.Errorf("Expected allowed, but failed with %+v", resp.Result)
	}
}

// ExpectFailsWith checks that a given admission response disallows the initiating request
// through and contains the provided string in its error message.
func ExpectFailsWith(t *testing.T, resp *admissionv1beta1.AdmissionResponse, contains string) {
	t.Helper()
	if resp.Allowed {
		t.Error("Expected denial, got allowed")
		return
	}
	if !strings.Contains(resp.Result.Message, contains) {
		t.Errorf("Expected failure containing %q got %q", contains, resp.Result.Message)
	}
}

// ExpectPatches checks that the provided serialized bytes consist of an expected
// collection of patches.  This is used to verify the mutations made in a mutating
// admission webhook's response.
func ExpectPatches(t *testing.T, a []byte, e []jsonpatch.JsonPatchOperation) {
	t.Helper()
	var got []jsonpatch.JsonPatchOperation

	err := json.Unmarshal(a, &got)
	if err != nil {
		t.Errorf("Failed to unmarshal patches: %s", err)
		return
	}

	// Give the patch a deterministic ordering.
	// Technically this can change the meaning, but the ordering is otherwise unstable
	// and difficult to test.
	sort.Slice(e, func(i, j int) bool {
		lhs, rhs := e[i], e[j]
		if lhs.Operation != rhs.Operation {
			return lhs.Operation < rhs.Operation
		}
		return lhs.Path < rhs.Path
	})
	sort.Slice(got, func(i, j int) bool {
		lhs, rhs := got[i], got[j]
		if lhs.Operation != rhs.Operation {
			return lhs.Operation < rhs.Operation
		}
		return lhs.Path < rhs.Path
	})

	// Even though diff is useful, seeing the whole objects
	// one under another helps a lot.
	t.Logf("Got Patches:  %#v", got)
	t.Logf("Want Patches: %#v", e)
	if diff := cmp.Diff(e, got, cmpopts.EquateEmpty()); diff != "" {
		t.Logf("diff Patches: %v", diff)
		t.Errorf("ExpectPatches (-want, +got) = %s", diff)
	}
}
