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

package dynamic

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/test/logging"
	"math/rand"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/test"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1b1testing "knative.dev/serving/pkg/testing/v1beta1"
	v1a1test "knative.dev/serving/test/v1alpha1"
	v1b1test "knative.dev/serving/test/v1beta1"
)

const (
	// Default for user containers in e2e tests. This value is lower than the general
	interval = 1 * time.Second
	timeout  = 10 * time.Minute
)

//GetGroupVersionResource returns schema.GroupVersionResource for a given version(v1alpha1/v1beta1) otherwise returns
//randomly either v1alpha1 or v1beta1
func GetGroupVersionResource(version string) schema.GroupVersionResource {
	versions := []schema.GroupVersionResource{
		v1alpha1.SchemeGroupVersion.WithResource("services"),
		v1beta1.SchemeGroupVersion.WithResource("services"),
	}
	if v1alpha1.SchemeGroupVersion.String() == version {
		return versions[0]
	} else if v1beta1.SchemeGroupVersion.String() == version {
		return versions[1]
	} else {
		source := rand.NewSource(time.Now().UnixNano())
		random := rand.New(source)
		return versions[random.Intn(len(versions))]
	}
}

// CreateServiceReady creates a new Service in state 'Ready'. This function expects Service and Image name
// passed in through 'uSvc' as unstructured.Unstructured. test.ResourceNames is returned with the Service, Domain, and Revision string.
func CreateServiceReady(t *testing.T, clients *test.Clients, uSvc *unstructured.Unstructured) test.ResourceNames {
	gvr := GetGroupVersionResource(uSvc.GetAPIVersion())
	svc, err := clients.Dynamic.Resource(gvr).Namespace(test.ServingNamespace).
		Create(uSvc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service %s: %#v", svc, err)
	}
	serviceName := svc.GetName()
	WaitForServiceReady(t, clients, serviceName)
	return GetService(t, clients, serviceName)
}

//GeService returns test.ResourceNames with serviceName,RevisionName and domain of a given service
func GetService(t *testing.T, clients *test.Clients, serviceName string) test.ResourceNames {
	//Choosing resource version dynamically
	gvr := GetGroupVersionResource("")

	service, err := clients.Dynamic.Resource(gvr).Namespace(test.ServingNamespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get %s: %#v", serviceName, err)
	}
	resource, err := populateResources(*service)
	if err != nil {
		t.Fatalf("Failed to populate resource: %#v", err)
	}
	return resource
}

//PatchServiceImage patches the existing service passed in with a new newImage. Returns the latest test.ResourceNames
func PatchServiceImage(t *testing.T, clients *test.Clients, serviceName string, newImage string) test.ResourceNames {
	//Choosing resource version dynamically
	gvr := GetGroupVersionResource("")
	var patchedBytes []byte
	service, err := clients.Dynamic.Resource(gvr).Namespace(test.ServingNamespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get %s: %#v", serviceName, err)
	}

	if v1beta1.SchemeGroupVersion.String() == service.GetAPIVersion() {
		serviceObject := v1beta1.Service{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(service.UnstructuredContent(), &serviceObject); err != nil {
			t.Fatalf("Failed to convert to object: %#v", err)
		}
		newServiceObject := serviceObject.DeepCopy()
		v1b1testing.WithServiceImage(newImage)(newServiceObject)
		patchedBytes, err = createPatch(serviceObject, newServiceObject)
		if err != nil {
			t.Fatalf("Failed to get bytes : %#v", err)
		}
	} else if v1alpha1.SchemeGroupVersion.String() == service.GetAPIVersion() {
		serviceObject := v1alpha1.Service{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(service.UnstructuredContent(), &serviceObject); err != nil {
			t.Fatalf("Failed to convert to object: %#v", err)
		}
		newServiceObject := serviceObject.DeepCopy()
		v1a1test.SetServiceImage(newServiceObject, newImage)
		patchedBytes, err = createPatch(serviceObject, newServiceObject)
		if err != nil {
			t.Fatalf("Failed to get bytes : %#v", err)
		}
	}

	gvr = GetGroupVersionResource(service.GetAPIVersion())
	patchedService, err := clients.Dynamic.Resource(gvr).Namespace(test.ServingNamespace).Patch(serviceName, types.JSONPatchType, patchedBytes, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to patch %s: %#v", serviceName, err)
	}
	resource, err := populateResources(*patchedService)
	if err != nil {
		t.Fatalf("Failed to populate resource: %#v", err)
	}
	return resource
}

// WaitForServiceState polls the status of the Service called serviceName
// from client every `interval` until `inState` returns `true` indicating it
// is done, returns an error or timeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForServiceState(t *testing.T, clients *test.Clients, serviceName string, desc string, inState func(svc *unstructured.Unstructured) (bool, error)) {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForServiceState/%s/%s", serviceName, desc))
	defer span.End()
	//Choosing resource version dynamically
	gvr := GetGroupVersionResource("")
	if v1beta1.SchemeGroupVersion.String() == gvr.GroupVersion().String() {
		var lastState *v1beta1.Service
		waitErr := wait.PollImmediate(interval, timeout, func() (bool, error) {
			var err error
			gvr := v1beta1.SchemeGroupVersion.WithResource("services")
			lastStateUnstruct, err := clients.Dynamic.Resource(gvr).Namespace(test.ServingNamespace).Get(serviceName, metav1.GetOptions{})
			if err != nil {
				return true, err
			}
			return inState(lastStateUnstruct)
		})
		if waitErr != nil {
			t.Fatal(errors.Wrapf(waitErr, "service %q is not in desired state, got: %+v", serviceName, lastState))
		}
	} else if v1alpha1.SchemeGroupVersion.String() == gvr.GroupVersion().String() {
		var lastState *v1alpha1.Service
		waitErr := wait.PollImmediate(interval, timeout, func() (bool, error) {
			var err error
			gvr := v1alpha1.SchemeGroupVersion.WithResource("services")
			lastStateUnstruct, err := clients.Dynamic.Resource(gvr).Namespace(test.ServingNamespace).Get(serviceName, metav1.GetOptions{})
			if err != nil {
				return true, err
			}
			return inState(lastStateUnstruct)
		})
		if waitErr != nil {
			t.Fatal(errors.Wrapf(waitErr, "service %q is not in desired state, got: %+v", serviceName, lastState))
		}
	}
}

// WaitForServiceLatestRevision takes a revision in through names and compares it to the current state of LatestCreatedRevisionName in Service.
// Once an update is detected in the LatestCreatedRevisionName, the function waits for the created revision to be set in LatestReadyRevisionName
// before returning the name of the revision.
func WaitForServiceLatestRevision(t *testing.T, clients *test.Clients, serviceName string, names test.ResourceNames) (string, error) {
	var revisionName string
	WaitForServiceState(t, clients, serviceName, "ServiceUpdatedWithRevision", func(svc *unstructured.Unstructured) (bool, error) {

		if v1beta1.SchemeGroupVersion.String() == svc.GetAPIVersion() {
			var lastState *v1beta1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(svc.UnstructuredContent(), &lastState); err != nil {
				t.Fatalf("Failed to convert to object: %#v", err)
			}
			if lastState.Status.LatestCreatedRevisionName != names.Revision {
				revisionName = lastState.Status.LatestCreatedRevisionName
				return true, nil
			}
		} else if v1alpha1.SchemeGroupVersion.String() == svc.GetAPIVersion() {
			var lastState *v1alpha1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(svc.UnstructuredContent(), &lastState); err != nil {
				t.Fatalf("Failed to convert to object: %#v", err)
			}
			if lastState.Status.LatestCreatedRevisionName != names.Revision {
				revisionName = lastState.Status.LatestCreatedRevisionName
				return true, nil
			}
		}
		return false, nil
	})
	WaitForServiceState(t, clients, serviceName, "ServiceReadyWithRevision", func(svc *unstructured.Unstructured) (bool, error) {
		if v1beta1.SchemeGroupVersion.String() == svc.GetAPIVersion() {
			var lastState *v1beta1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(svc.UnstructuredContent(), &lastState); err != nil {
				t.Fatalf("Failed to convert to object: %#v", err)
			}
			return lastState.Status.LatestReadyRevisionName == revisionName, nil
		} else if v1alpha1.SchemeGroupVersion.String() == svc.GetAPIVersion() {
			var lastState *v1alpha1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(svc.UnstructuredContent(), &lastState); err != nil {
				t.Fatalf("Failed to convert to object: %#v", err)
			}
			return lastState.Status.LatestReadyRevisionName == revisionName, nil
		}
		return false, nil
	})

	return revisionName, nil
}

// WaitForServiceReady verifies the status of the Service called name is in `Ready` state.
// This is the non-polling variety of WaitForServiceState.
func WaitForServiceReady(t *testing.T, clients *test.Clients, serviceName string) {
	WaitForServiceState(t, clients, serviceName, "ServiceReadyWithRevision", func(svc *unstructured.Unstructured) (b bool, e error) {
		if v1beta1.SchemeGroupVersion.String() == svc.GetAPIVersion() {
			var lastState *v1beta1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(svc.UnstructuredContent(), &lastState); err != nil {
				t.Fatalf("Failed to convert to object: %#v", err)
			}
			return v1b1test.IsServiceReady(lastState)
		} else if v1alpha1.SchemeGroupVersion.String() == svc.GetAPIVersion() {
			var lastState *v1alpha1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(svc.UnstructuredContent(), &lastState); err != nil {
				t.Fatalf("Failed to convert to object: %#v", err)
			}
			return v1a1test.IsServiceReady(lastState)
		}
		return false, nil
	})
}

func createPatch(cur, desired interface{}) ([]byte, error) {
	patch, err := duck.CreatePatch(cur, desired)
	if err != nil {
		return nil, err
	}
	return patch.MarshalJSON()
}

func populateResources(service unstructured.Unstructured) (test.ResourceNames, error) {
	resource := test.ResourceNames{}
	if v1beta1.SchemeGroupVersion.String() == service.GetAPIVersion() {
		var serviceObject v1beta1.Service
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(service.UnstructuredContent(), &serviceObject); err != nil {
			return resource, err
		}
		resource.Domain = serviceObject.Status.URL.Host
		resource.Revision = serviceObject.Status.LatestCreatedRevisionName
		resource.Service = serviceObject.Name
	} else if v1alpha1.SchemeGroupVersion.String() == service.GetAPIVersion() {
		var serviceObject v1alpha1.Service
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(service.UnstructuredContent(), &serviceObject); err != nil {
			return resource, err
		}
		resource.Domain = serviceObject.Status.URL.Host
		resource.Revision = serviceObject.Status.LatestCreatedRevisionName
		resource.Service = serviceObject.Name
	}
	return resource, nil
}
