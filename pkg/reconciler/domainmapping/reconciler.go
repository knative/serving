/*
Copyright 2020 The Knative Authors

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

package domainmapping

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netclientset "knative.dev/networking/pkg/client/clientset/versioned"
	networkinglisters "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/resolver"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	domainmappingreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1alpha1/domainmapping"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
	"knative.dev/serving/pkg/reconciler/domainmapping/resources"
)

// Reconciler implements controller.Reconciler for DomainMapping resources.
type Reconciler struct {
	ingressLister networkinglisters.IngressLister
	netclient     netclientset.Interface
	resolver      *resolver.URIResolver
}

// Check that our Reconciler implements Interface
var _ domainmappingreconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (r *Reconciler) ReconcileKind(ctx context.Context, dm *v1alpha1.DomainMapping) reconciler.Event {
	logger := logging.FromContext(ctx)
	logger.Debugf("Reconciling DomainMapping %s/%s", dm.Namespace, dm.Name)

	// TODO(https://github.com/knative/serving/issues/10247)
	dm.Status.MarkTLSNotEnabled("AutoTLS for DomainMapping is not implemented")

	// Defensively assume the ingress is not configured until we manage to
	// successfully reconcile it below. This avoids error cases where we fail
	// before we've reconciled the ingress and get a new ObservedGeneration but
	// still have Ingress Ready: True.
	if dm.GetObjectMeta().GetGeneration() != dm.Status.ObservedGeneration {
		dm.Status.MarkIngressNotConfigured()
	}

	// Mapped URL is the metadata.name of the DomainMapping.
	url := &apis.URL{Scheme: "http", Host: dm.Name}
	dm.Status.URL = url
	dm.Status.Address = &duckv1.Addressable{URL: url}

	// IngressClass can be set via annotations or in the config map.
	ingressClass := dm.Annotations[networking.IngressClassAnnotationKey]
	if ingressClass == "" {
		ingressClass = config.FromContext(ctx).Network.DefaultIngressClass
	}

	// To prevent Ingress hostname collision, require that we can create, or
	// already own, a cluster-wide domain claim.
	if err := r.reconcileDomainClaim(ctx, dm); err != nil {
		return err
	}

	// Resolve the spec.Ref to a URI following the Addressable contract.
	targetHost, targetBackendSvc, err := r.resolveRef(ctx, dm)
	if err != nil {
		return err
	}

	// Reconcile the Ingress resource corresponding to the requested Mapping.
	logger.Debugf("Mapping %s to ref %s/%s (host: %q, svc: %q)", url, dm.Spec.Ref.Namespace, dm.Spec.Ref.Name, targetHost, targetBackendSvc)
	desired := resources.MakeIngress(dm, targetBackendSvc, targetHost, ingressClass, nil /* tls */)
	ingress, err := r.reconcileIngress(ctx, dm, desired)
	if err != nil {
		return err
	}

	// Check that the Ingress status reflects the latest ingress applied and propagate status if so.
	if ingress.GetObjectMeta().GetGeneration() != ingress.Status.ObservedGeneration {
		dm.Status.MarkIngressNotConfigured()
	} else {
		dm.Status.PropagateIngressStatus(ingress.Status)
	}

	return err
}

func (r *Reconciler) reconcileIngress(ctx context.Context, dm *v1alpha1.DomainMapping, desired *netv1alpha1.Ingress) (*netv1alpha1.Ingress, error) {
	recorder := controller.GetEventRecorder(ctx)
	ingress, err := r.ingressLister.Ingresses(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		ingress, err = r.netclient.NetworkingV1alpha1().Ingresses(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			recorder.Eventf(dm, corev1.EventTypeWarning, "CreationFailed", "Failed to create Ingress: %v", err)
			return nil, fmt.Errorf("failed to create Ingress: %w", err)
		}

		recorder.Eventf(dm, corev1.EventTypeNormal, "Created", "Created Ingress %q", ingress.GetName())
		return ingress, nil
	} else if err != nil {
		return nil, err
	} else if !equality.Semantic.DeepEqual(ingress.Spec, desired.Spec) ||
		!equality.Semantic.DeepEqual(ingress.Annotations, desired.Annotations) {

		// Don't modify the informers copy
		origin := ingress.DeepCopy()
		origin.Spec = desired.Spec
		origin.Annotations = desired.Annotations
		updated, err := r.netclient.NetworkingV1alpha1().Ingresses(origin.Namespace).Update(ctx, origin, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to update Ingress: %w", err)
		}
		return updated, nil
	}

	return ingress, err
}

func (r *Reconciler) resolveRef(ctx context.Context, dm *v1alpha1.DomainMapping) (host, backendSvc string, err error) {
	resolved, err := r.resolver.URIFromKReference(ctx, &dm.Spec.Ref, dm)
	if err != nil {
		dm.Status.MarkReferenceNotResolved(err.Error())
		return "", "", fmt.Errorf("resolving reference: %w", err)
	}

	// Since we do not support path-based routing in domain mappings, we cannot
	// support target references that contain a path.
	if strings.TrimSuffix(resolved.Path, "/") != "" {
		dm.Status.MarkReferenceNotResolved(fmt.Sprintf("resolved URI %q contains a path", resolved))
		return "", "", fmt.Errorf("resolved URI %q contains a path", resolved)
	}

	// The resolved hostname must be of the form {name}.{namespace}.svc.{suffix},
	// which is the standard DNS address given by kubernetes to services. We will
	// use `name` from this resolved hostname to determine the backend service
	// name for the KIngress.
	// TODO(julz) in the future we may support addressables that are not created
	// from Services, in which case we would need to dynamically create the
	// an ExternalName Service for the KIngress to use.
	requiredSuffix := ".svc." + network.GetClusterDomainName()
	parts := strings.Split(strings.TrimSuffix(resolved.Host, requiredSuffix), ".")
	if !strings.HasSuffix(resolved.Host, requiredSuffix) || len(parts) != 2 {
		dm.Status.MarkReferenceNotResolved(fmt.Sprintf("resolved URI %q must be of the form {name}.{namespace}%s", resolved, requiredSuffix))
		return "", "", fmt.Errorf("resolved URI %q must be of the form {name}.{namespace}%s", resolved, requiredSuffix)
	}

	// If the namespace part of the target isn't the same as the DomainMapping
	// we'd need to create a cross-namespace KIngress, which isn't supported.
	if parts[1] != dm.Namespace {
		dm.Status.MarkReferenceNotResolved(fmt.Sprintf("resolved URI %q must be in same namespace as DomainMapping", resolved))
		return "", "", fmt.Errorf("resolved URI %q must be in same namespace as DomainMapping", resolved)
	}

	dm.Status.MarkReferenceResolved()
	return resolved.Host, parts[0], nil
}

func (r *Reconciler) reconcileDomainClaim(ctx context.Context, dm *v1alpha1.DomainMapping) error {
	dc, err := r.netclient.NetworkingV1alpha1().ClusterDomainClaims().Get(ctx, dm.Name, metav1.GetOptions{})
	if apierrs.IsNotFound(err) {
		if dc, err = r.netclient.NetworkingV1alpha1().ClusterDomainClaims().Create(ctx, resources.MakeDomainClaim(dm), metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("failed to create ClusterDomainClaim: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get ClusterDomainClaim: %w", err)
	}

	if !metav1.IsControlledBy(dc, dm) {
		dm.Status.MarkDomainClaimNotOwned()
		return fmt.Errorf("domain mapping: %q does not own matching cluster domain claim", dm.Name)
	}

	dm.Status.MarkDomainClaimed()
	return nil
}
