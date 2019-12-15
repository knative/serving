/*
Copyright 2019 The Knative Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    https://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package istio

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	istioclientset "knative.dev/pkg/client/istio/clientset/versioned"
	istiolisters "knative.dev/pkg/client/istio/listers/networking/v1alpha3"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
)

// ServiceEntryAccessor is an interface for accessing ServiceEntry.
type ServiceEntryAccessor interface {
	GetIstioClient() istioclientset.Interface
	GetServiceEntryLister() istiolisters.ServiceEntryLister
}

// ReconcileServiceEntry reconciles ServiceEntry to the desired status.
func ReconcileServiceEntry(ctx context.Context, owner kmeta.Accessor, desired *v1alpha3.ServiceEntry, seAccessor ServiceEntryAccessor) (*v1alpha3.ServiceEntry, error) {
	logger := logging.FromContext(ctx)
	recorder := controller.GetEventRecorder(ctx)
	if recorder == nil {
		return nil, fmt.Errorf("recoder for reconcilging ServiceEntry %q/%q is not created", desired.Namespace, desired.Name)
	}
	ns := desired.Namespace
	name := desired.Name
	se, err := seAccessor.GetServiceEntryLister().ServiceEntries(ns).Get(name)
	if apierrs.IsNotFound(err) {
		if se, err = seAccessor.GetIstioClient().NetworkingV1alpha3().ServiceEntries(ns).Create(desired); err != nil {
			logger.Errorw("Failed to create Servic Entry", zap.Error(err))
			recorder.Eventf(owner, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create ServiceEntry %q/%q: %v", ns, name, err)
			return nil, err
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Created", "Created ServiceEntry %q", desired.Name)
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(se, owner) {
		// Surface an error in the ClusterIngress's status, and return an error.
		return nil, fmt.Errorf("sks: %q does not own ServiceEntry: %q", owner.GetName(), name)
	} else if !equality.Semantic.DeepEqual(se.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := se.DeepCopy()
		existing.Spec = desired.Spec
		se, err = seAccessor.GetIstioClient().NetworkingV1alpha3().ServiceEntries(ns).Update(existing)
		if err != nil {
			logger.Errorw("Failed to update ServiceEntry", zap.Error(err))
			return nil, err
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Updated", "Updated status for ServiceEntry %q/%q", ns, name)
	}
	return se, nil
}
