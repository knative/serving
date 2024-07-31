//go:build e2e
// +build e2e

/*
Copyright 2023 The Knative Authors

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

package securedefaults

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"knative.dev/pkg/ptr"
	. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestSecureDefaults(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create service with default SecurityContext: %v: %v", names.Service, err)
	}

	revisionSC := resources.Revision.Spec.Containers[0].SecurityContext
	if revisionSC == nil {
		t.Fatal("Container SecurityContext was nil, should have been defaulted.")
	}
	if len(revisionSC.Capabilities.Drop) != 1 || revisionSC.Capabilities.Drop[0] != "ALL" {
		t.Errorf("Expected to Drop 'ALL' capability: %v", revisionSC.Capabilities)
	}
	if revisionSC.AllowPrivilegeEscalation == nil || *revisionSC.AllowPrivilegeEscalation {
		t.Errorf("Expected allowPrivilegeEscalation: false, got %v", revisionSC.AllowPrivilegeEscalation)
	}
}

func TestUnsafePermitted(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	withDefaultUnsafeContext := WithSecurityContext(&v1.SecurityContext{
		Capabilities: &v1.Capabilities{
			Drop: []v1.Capability{},
		},
		RunAsNonRoot:             ptr.Bool(false),
		AllowPrivilegeEscalation: ptr.Bool(true),
		SeccompProfile: &v1.SeccompProfile{
			Type: v1.SeccompProfileTypeUnconfined,
		},
	})

	resources, err := v1test.CreateServiceReady(t, clients, &names, withDefaultUnsafeContext)
	if err != nil {
		t.Fatalf("Failed to create service with explicit k8s default SecurityContext: %v: %v", names.Service, err)
	}

	revisionSC := resources.Revision.Spec.Containers[0].SecurityContext
	if revisionSC == nil {
		t.Fatal("Container SecurityContext was nil, requested non-nil.")
	}
	if len(revisionSC.Capabilities.Drop) != 0 {
		t.Errorf("Expected to Drop no capabilities (empty list): %v", revisionSC.Capabilities)
	}
	if revisionSC.AllowPrivilegeEscalation == nil || !*revisionSC.AllowPrivilegeEscalation {
		t.Errorf("Expected allowPrivilegeEscalation: true, got %v", revisionSC.AllowPrivilegeEscalation)
	}
	if revisionSC.SeccompProfile == nil || revisionSC.SeccompProfile.Type != v1.SeccompProfileTypeUnconfined {
		t.Errorf("Expected seccompProfile to be Unconfined, got: %v", revisionSC.SeccompProfile)
	}
}
