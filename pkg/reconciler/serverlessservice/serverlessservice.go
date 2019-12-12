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

package serverlessservice

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	rbase "knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources"
	presources "knative.dev/serving/pkg/resources"
)

const reconcilerName = "ServerlessServices"

// reconciler implements controller.Reconciler for Service resources.
type reconciler struct {
	*rbase.Base

	// listers index properties about resources
	sksLister       listers.ServerlessServiceLister
	serviceLister   corev1listers.ServiceLister
	endpointsLister corev1listers.EndpointsLister

	// Used to get PodScalables from object references.
	psInformerFactory duck.InformerFactory
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Revision resource
// with the current status of the resource.
func (r *reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorw("Invalid resource key", zap.Error(err))
		return nil
	}

	logger.Debug("Reconciling SKS resource")
	// Get the current SKS resource.
	original, err := r.sksLister.ServerlessServices(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Info("SKS resource in work queue no longer exists")
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informers copy.
	sks := original.DeepCopy()
	reconcileErr := r.reconcile(ctx, sks)
	if reconcileErr != nil {
		r.Recorder.Eventf(sks, corev1.EventTypeWarning, "UpdateFailed", "InternalError: %v", reconcileErr.Error())
	}
	if !equality.Semantic.DeepEqual(sks.Status, original.Status) {
		if err := r.updateStatus(original, sks); err != nil {
			r.Recorder.Eventf(sks, corev1.EventTypeWarning, "UpdateFailed", "Failed to update status: %v", err)
			return err
		}
		r.Recorder.Eventf(sks, corev1.EventTypeNormal, "Updated", "Successfully updated ServerlessService %q", key)
	}
	return reconcileErr
}

func (r *reconciler) reconcile(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)
	// Don't reconcile if we're being deleted.
	if sks.GetDeletionTimestamp() != nil {
		return nil
	}

	sks.SetDefaults(ctx)
	sks.Status.InitializeConditions()

	for i, fn := range []func(context.Context, *netv1alpha1.ServerlessService) error{
		r.reconcilePrivateService, // First make sure our data source is setup.
		r.reconcilePublicService,
		r.reconcilePublicEndpoints,
	} {
		if err := fn(ctx, sks); err != nil {
			logger.Debugw(strconv.Itoa(i)+": reconcile failed", zap.Error(err))
			return err
		}
	}
	sks.Status.ObservedGeneration = sks.Generation
	return nil
}

func (r *reconciler) updateStatus(existing *netv1alpha1.ServerlessService, desired *netv1alpha1.ServerlessService) error {
	existing = existing.DeepCopy()
	return rbase.RetryUpdateConflicts(func(attempts int) (err error) {
		// The first iteration tries to use the informer's state, subsequent attempts fetch the latest state via API.
		if attempts > 0 {
			existing, err = r.ServingClientSet.NetworkingV1alpha1().ServerlessServices(desired.Namespace).Get(desired.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		// If there's nothing to update, just return.
		if reflect.DeepEqual(existing.Status, desired.Status) {
			return nil
		}

		existing.Status = desired.Status
		_, err = r.ServingClientSet.NetworkingV1alpha1().ServerlessServices(existing.Namespace).UpdateStatus(existing)
		return err
	})
}

func (r *reconciler) reconcilePublicService(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)

	sn := sks.Name
	srv, err := r.serviceLister.Services(sks.Namespace).Get(sn)
	if apierrs.IsNotFound(err) {
		logger.Infof("K8s public service %s does not exist; creating.", sn)
		// We've just created the service, so it has no endpoints.
		sks.Status.MarkEndpointsNotReady("CreatingPublicService")
		srv = resources.MakePublicService(sks)
		_, err := r.KubeClientSet.CoreV1().Services(sks.Namespace).Create(srv)
		if err != nil {
			return fmt.Errorf("failed to create public K8s Service: %w", err)
		}
		logger.Info("Created public K8s service: ", sn)
	} else if err != nil {
		return fmt.Errorf("failed to get public K8s Service: %w", err)
	} else if !metav1.IsControlledBy(srv, sks) {
		sks.Status.MarkEndpointsNotOwned("Service", sn)
		return fmt.Errorf("SKS: %s does not own Service: %s", sks.Name, sn)
	} else {
		tmpl := resources.MakePublicService(sks)
		want := srv.DeepCopy()
		want.Spec.Ports = tmpl.Spec.Ports
		want.Spec.Selector = nil

		if !equality.Semantic.DeepEqual(want.Spec, srv.Spec) {
			logger.Info("Public K8s Service changed; reconciling: ", sn, cmp.Diff(want.Spec, srv.Spec))
			if _, err = r.KubeClientSet.CoreV1().Services(sks.Namespace).Update(want); err != nil {
				return fmt.Errorf("failed to update public K8s Service: %w", err)
			}
		}
	}
	sks.Status.ServiceName = sn
	logger.Debug("Done reconciling public K8s service: ", sn)
	return nil
}

func (r *reconciler) reconcilePublicEndpoints(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)

	var (
		srcEps                *corev1.Endpoints
		foundServingEndpoints bool
	)
	activatorEps, err := r.endpointsLister.Endpoints(system.Namespace()).Get(networking.ActivatorServiceName)
	if err != nil {
		return fmt.Errorf("failed to get activator service endpoints: %w", err)
	}
	logger.Debug("Activator endpoints: ", spew.Sprint(activatorEps))

	psn := sks.Status.PrivateServiceName
	pvtEps, err := r.endpointsLister.Endpoints(sks.Namespace).Get(psn)
	if err != nil {
		return fmt.Errorf("failed to get private K8s Service endpoints: %w", err)
	}
	// We still might be "ready" even if in proxy mode,
	// if proxy mode is by means of burst capacity handling.
	pvtReady := presources.ReadyAddressCount(pvtEps)
	if pvtReady > 0 {
		foundServingEndpoints = true
	}

	// The logic below is as follows:
	// if mode == serve:
	//   if len(private_service_endpoints) > 0:
	//     srcEps = private_service_endpoints
	//   else:
	//     srcEps = activator_endpoints
	// else:
	//    srcEps = activator_endpoints
	// The reason for this is, we don't want to leave the public service endpoints empty,
	// since those endpoints are the ones programmed into the VirtualService.
	//
	switch sks.Spec.Mode {
	case netv1alpha1.SKSOperationModeServe:
		// We should have successfully reconciled the private service if we're here
		// which means that we'd have the name assigned in Status.
		logger.Debugf("Private endpoints: %s", spew.Sprint(pvtEps))
		// Serving but no ready endpoints.
		if pvtReady == 0 {
			logger.Info(psn + " is in mode Serve but has no endpoints, using Activator endpoints for now")
			srcEps = activatorEps
		} else {
			// Serving & have endpoints ready.
			srcEps = pvtEps
		}
	case netv1alpha1.SKSOperationModeProxy:
		srcEps = activatorEps
	}

	sn := sks.Name
	eps, err := r.endpointsLister.Endpoints(sks.Namespace).Get(sn)

	if apierrs.IsNotFound(err) {
		logger.Infof("Public endpoints %s does not exist; creating.", sn)
		sks.Status.MarkEndpointsNotReady("CreatingPublicEndpoints")
		if _, err = r.KubeClientSet.CoreV1().Endpoints(sks.Namespace).Create(resources.MakePublicEndpoints(sks, srcEps)); err != nil {
			return fmt.Errorf("failed to create public K8s Endpoints: %w", err)
		}
		logger.Info("Created K8s Endpoints: ", sn)
	} else if err != nil {
		return fmt.Errorf("failed to get public K8s Endpoints: %w", err)
	} else if !metav1.IsControlledBy(eps, sks) {
		sks.Status.MarkEndpointsNotOwned("Endpoints", sn)
		return fmt.Errorf("SKS: %s does not own Endpoints: %s", sks.Name, sn)
	} else {
		wantSubsets := resources.FilterSubsetPorts(sks, srcEps.Subsets)
		if !equality.Semantic.DeepEqual(wantSubsets, eps.Subsets) {
			want := eps.DeepCopy()
			want.Subsets = wantSubsets
			logger.Info("Public K8s Endpoints changed; reconciling: ", sn)
			if _, err = r.KubeClientSet.CoreV1().Endpoints(sks.Namespace).Update(want); err != nil {
				return fmt.Errorf("failed to update public K8s Endpoints: %w", err)
			}
		}
	}
	if foundServingEndpoints {
		sks.Status.MarkEndpointsReady()
	} else {
		logger.Infof("Endpoints %s has no ready endpoints", sn)
		sks.Status.MarkEndpointsNotReady("NoHealthyBackends")
	}
	// If we have no backends or if we're in the proxy mode, then
	// activator backs this revision.
	if !foundServingEndpoints || sks.Spec.Mode == netv1alpha1.SKSOperationModeProxy {
		sks.Status.MarkActivatorEndpointsPopulated()
	} else {
		sks.Status.MarkActivatorEndpointsRemoved()
	}

	logger.Debug("Done reconciling public K8s endpoints: ", sn)
	return nil
}

func (r *reconciler) privateService(sks *netv1alpha1.ServerlessService) (*corev1.Service, error) {
	// The code below is for backwards compatibility, when we had
	// GenerateName for the private services.
	svcs, err := r.serviceLister.Services(sks.Namespace).List(labels.SelectorFromSet(map[string]string{
		networking.SKSLabelKey:    sks.Name,
		networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
	}))
	if err != nil {
		return nil, err
	}
	switch l := len(svcs); l {
	case 0:
		return nil, apierrs.NewNotFound(corev1.Resource("Services"), sks.Name)
	case 1:
		return svcs[0], nil
	default:
		// We encountered more than one. Keep the one that is in the SKS status and delete the others.
		var ret *corev1.Service
		for _, s := range svcs {
			if s.Name == sks.Status.PrivateServiceName {
				ret = s
				continue
			}
			// If we don't control it, don't delete it.
			if metav1.IsControlledBy(s, sks) {
				r.KubeClientSet.CoreV1().Services(sks.Namespace).Delete(s.Name, &metav1.DeleteOptions{})
			}
		}
		return ret, nil
	}
}

func (r *reconciler) reconcilePrivateService(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)

	selector, err := r.getSelector(sks)
	if err != nil {
		return fmt.Errorf("error retrieving deployment selector spec: %w", err)
	}

	svc, err := r.privateService(sks)
	if apierrs.IsNotFound(err) {
		logger.Info("SKS has no private service; creating.")
		sks.Status.MarkEndpointsNotReady("CreatingPrivateService")
		svc = resources.MakePrivateService(sks, selector)
		svc, err = r.KubeClientSet.CoreV1().Services(sks.Namespace).Create(svc)
		if err != nil {
			return fmt.Errorf("failed to create private K8s Service: %w", err)
		}
		logger.Info("Created private K8s service: ", svc.Name)
	} else if err != nil {
		return fmt.Errorf("failed to get private K8s Service: %w", err)
	} else if !metav1.IsControlledBy(svc, sks) {
		sks.Status.MarkEndpointsNotOwned("Service", svc.Name)
		return fmt.Errorf("SKS: %s does not own Service: %s", sks.Name, svc.Name)
	} else {
		tmpl := resources.MakePrivateService(sks, selector)
		want := svc.DeepCopy()
		// Our controller manages only part of spec, so set the fields we own.
		want.Spec.Ports = tmpl.Spec.Ports
		want.Spec.Selector = tmpl.Spec.Selector

		if !equality.Semantic.DeepEqual(svc.Spec, want.Spec) {
			sks.Status.MarkEndpointsNotReady("UpdatingPrivateService")
			logger.Infof("Private K8s Service changed %s; reconciling: ", svc.Name)
			if _, err = r.KubeClientSet.CoreV1().Services(sks.Namespace).Update(want); err != nil {
				return fmt.Errorf("failed to update private K8s Service: %w", err)
			}
		}
	}

	sks.Status.PrivateServiceName = svc.Name
	logger.Debug("Done reconciling private K8s service: ", svc.Name)
	return nil
}

func (r *reconciler) getSelector(sks *netv1alpha1.ServerlessService) (map[string]string, error) {
	scale, err := presources.GetScaleResource(sks.Namespace, sks.Spec.ObjectRef, r.psInformerFactory)
	if err != nil {
		return nil, err
	}
	return scale.Spec.Selector.MatchLabels, nil
}
