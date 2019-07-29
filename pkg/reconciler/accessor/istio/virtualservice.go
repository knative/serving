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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"knative.dev/pkg/apis/istio/v1alpha3"
	sharedclientset "knative.dev/pkg/client/clientset/versioned"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
)

// VirtualServiceAccessor is an interface for accessing VirtualService.
type VirtualServiceAccessor interface {
	GetVirtualServiceClient() sharedclientset.Interface
	GetVirtualServiceLister() istiolisters.VirtualServiceLister
}

// ReconcileVirtualService reconciles VirtiualService to the desired status.
func ReconcileVirtualService(ctx context.Context, owner kmeta.Accessor, desired *v1alpha3.VirtualService,
	recorder record.EventRecorder, vsAccessor VirtualServiceAccessor) (*v1alpha3.VirtualService, error) {

	logger := logging.FromContext(ctx)
	ns := desired.Namespace
	name := desired.Name
	vs, err := vsAccessor.GetVirtualServiceLister().VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		_, err = vsAccessor.GetVirtualServiceClient().NetworkingV1alpha3().VirtualServices(ns).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create VirtualService", zap.Error(err))
			recorder.Eventf(owner, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %q/%q: %v", ns, name, err)
			return nil, err
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Created", "Created VirtualService %q", desired.Name)
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(vs, owner) {
		// Surface an error in the ClusterIngress's status, and return an error.
		return nil, fmt.Errorf("ingress: %q does not own VirtualService: %q", owner.GetName(), name)
	} else if !equality.Semantic.DeepEqual(vs.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := vs.DeepCopy()
		existing.Spec = desired.Spec
		vs, err = vsAccessor.GetVirtualServiceClient().NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		if err != nil {
			logger.Errorw("Failed to update VirtualService", zap.Error(err))
			return nil, err
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Updated", "Updated status for VirtualService %q/%q", ns, name)
	}
	return vs, nil
}
