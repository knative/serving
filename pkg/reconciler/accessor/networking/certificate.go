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

package networking

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	clientset "knative.dev/networking/pkg/client/clientset/versioned"
	listers "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
)

// CertificateAccessor is an interface for accessing Knative Certificate.
type CertificateAccessor interface {
	GetNetworkingClient() clientset.Interface
	GetCertificateLister() listers.CertificateLister
}

// ReconcileCertificate reconciles Certificate to the desired status.
func ReconcileCertificate(ctx context.Context, owner kmeta.Accessor, desired *v1alpha1.Certificate,
	certAccessor CertificateAccessor) (*v1alpha1.Certificate, error) {

	recorder := controller.GetEventRecorder(ctx)
	if recorder == nil {
		return nil, fmt.Errorf("recorder for reconciling Certificate %s/%s is not created", desired.Namespace, desired.Name)
	}
	cert, err := certAccessor.GetCertificateLister().Certificates(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		cert, err = certAccessor.GetNetworkingClient().NetworkingV1alpha1().Certificates(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			recorder.Eventf(owner, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Certificate %s/%s: %v", desired.Namespace, desired.Name, err)
			return nil, fmt.Errorf("failed to create Certificate: %w", err)
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Created", "Created Certificate %s/%s", cert.Namespace, cert.Name)
		return cert, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get Certificate: %w", err)
	} else if !metav1.IsControlledBy(cert, owner) {
		// Return an error with NotControlledBy information.
		return nil, kaccessor.NewAccessorError(
			fmt.Errorf("owner: %s with Type %T does not own Certificate: %q", owner.GetName(), owner, cert.Name),
			kaccessor.NotOwnResource)
	} else if !equality.Semantic.DeepEqual(cert.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := cert.DeepCopy()
		existing.Spec = desired.Spec
		cert, err = certAccessor.GetNetworkingClient().NetworkingV1alpha1().Certificates(existing.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			recorder.Eventf(owner, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update Certificate %s/%s: %v", existing.Namespace, existing.Name, err)
			return nil, fmt.Errorf("failed to update Certificate: %w", err)
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Updated",
			"Updated Spec for Certificate %s/%s", existing.Namespace, existing.Name)
	}
	return cert, nil
}
