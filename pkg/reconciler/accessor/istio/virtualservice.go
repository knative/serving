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

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	istioclientset "knative.dev/serving/pkg/client/istio/clientset/versioned"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
)

// VirtualServiceAccessor is an interface for accessing VirtualService.
type VirtualServiceAccessor interface {
	GetIstioClient() istioclientset.Interface
	GetVirtualServiceLister() istiolisters.VirtualServiceLister
}

func hasDesiredDiff(current, desired *v1alpha3.VirtualService) bool {
	return !equality.Semantic.DeepEqual(current.Spec, desired.Spec) ||
		!equality.Semantic.DeepEqual(current.Labels, desired.Labels) ||
		!equality.Semantic.DeepEqual(current.Annotations, desired.Annotations)
}

// ReconcileVirtualService reconciles VirtiualService to the desired status.
func ReconcileVirtualService(ctx context.Context, owner kmeta.Accessor, desired *v1alpha3.VirtualService,
	vsAccessor VirtualServiceAccessor) (*v1alpha3.VirtualService, error) {

	recorder := controller.GetEventRecorder(ctx)
	if recorder == nil {
		return nil, fmt.Errorf("recoder for reconciling VirtualService %s/%s is not created", desired.Namespace, desired.Name)
	}
	ns := desired.Namespace
	name := desired.Name
	vs, err := vsAccessor.GetVirtualServiceLister().VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		vs, err = vsAccessor.GetIstioClient().NetworkingV1alpha3().VirtualServices(ns).Create(desired)
		if err != nil {
			recorder.Eventf(owner, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %s/%s: %v", ns, name, err)
			return nil, fmt.Errorf("failed to create VirtualService: %w", err)
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Created", "Created VirtualService %q", desired.Name)
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(vs, owner) {
		// Return an error with NotControlledBy information.
		return nil, kaccessor.NewAccessorError(
			fmt.Errorf("owner: %s with Type %T does not own VirtualService: %q", owner.GetName(), owner, name),
			kaccessor.NotOwnResource)
	} else if hasDesiredDiff(vs, desired) {
		// Don't modify the informers copy
		existing := vs.DeepCopy()
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations
		vs, err = vsAccessor.GetIstioClient().NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		if err != nil {
			return nil, fmt.Errorf("failed to update VirtualService: %w", err)
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Updated", "Updated VirtualService %s/%s", ns, name)
	}
	return vs, nil
}
