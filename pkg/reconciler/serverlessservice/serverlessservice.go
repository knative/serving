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

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis/duck"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/networking"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	rbase "github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/serverlessservice/resources"
	presources "github.com/knative/serving/pkg/resources"
	perrors "github.com/pkg/errors"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
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
	// Convert the namespace/name string into a distinct namespace and name
	logger := logging.FromContext(ctx)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorf("Invalid resource key: %s", key)
		return nil
	}

	logger.Debugf("Reconciling SKS resource: %s", key)
	// Get the current SKS resource.
	original, err := r.sksLister.ServerlessServices(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("SKS resource %q in work queue no longer exists", key)
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
		if _, err := r.updateStatus(sks); err != nil {
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
			logger.Debugw(fmt.Sprintf("%d: reconcile failed", i), zap.Error(err))
			return err
		}
	}
	sks.Status.ObservedGeneration = sks.Generation
	return nil
}

func (r *reconciler) updateStatus(sks *netv1alpha1.ServerlessService) (*netv1alpha1.ServerlessService, error) {
	original, err := r.sksLister.ServerlessServices(sks.Namespace).Get(sks.Name)
	if err != nil {
		return nil, err
	}
	if reflect.DeepEqual(original.Status, sks.Status) {
		return original, nil
	}
	r.Logger.Debugf("StatusDiff: %s", cmp.Diff(original.Status, sks.Status))
	original = original.DeepCopy()
	original.Status = sks.Status
	return r.ServingClientSet.NetworkingV1alpha1().ServerlessServices(sks.Namespace).UpdateStatus(original)
}

func (r *reconciler) reconcilePublicService(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)

	sn := sks.Name
	srv, err := r.serviceLister.Services(sks.Namespace).Get(sn)
	if errors.IsNotFound(err) {
		logger.Infof("K8s service %s does not exist; creating.", sn)
		// We've just created the service, so it has no endpoints.
		sks.Status.MarkEndpointsNotReady("CreatingPublicService")
		srv = resources.MakePublicService(sks)
		_, err := r.KubeClientSet.CoreV1().Services(sks.Namespace).Create(srv)
		if err != nil {
			logger.Errorw(fmt.Sprint("Error creating K8s Service:", sn), zap.Error(err))
			return err
		}
		logger.Info("Created K8s service: ", sn)
	} else if err != nil {
		logger.Errorw(fmt.Sprint("Error getting K8s Service:", sn), zap.Error(err))
		return err
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
				logger.Errorw(fmt.Sprint("Error updating public K8s Service:", sn), zap.Error(err))
				return err
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
		activatorEps          *corev1.Endpoints
		err                   error
		foundServingEndpoints bool
	)
	activatorEps, err = r.endpointsLister.Endpoints(system.Namespace()).Get(activator.K8sServiceName)
	if err != nil {
		logger.Errorw("Error obtaining activator service endpoints", zap.Error(err))
		return err
	}
	logger.Debugf("Activator endpoints: %s", spew.Sprint(activatorEps))

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
		psn := sks.Status.PrivateServiceName
		srcEps, err = r.endpointsLister.Endpoints(sks.Namespace).Get(sks.Status.PrivateServiceName)
		if err != nil {
			logger.Errorw(fmt.Sprintf("Error obtaining private service endpoints: %s", psn), zap.Error(err))
			return err
		}
		logger.Debugf("Private endpoints: %s", spew.Sprint(srcEps))
		if r := presources.ReadyAddressCount(srcEps); r == 0 {
			logger.Infof("%s is in mode Serve but has no endpoints, using Activator endpoints for now", psn)
			srcEps = activatorEps
		} else {
			foundServingEndpoints = true
		}
	case netv1alpha1.SKSOperationModeProxy:
		srcEps = activatorEps
	}

	sn := sks.Name
	eps, err := r.endpointsLister.Endpoints(sks.Namespace).Get(sn)

	if errors.IsNotFound(err) {
		logger.Infof("K8s endpoints %s does not exist; creating.", sn)
		sks.Status.MarkEndpointsNotReady("CreatingPublicEndpoints")
		eps, err = r.KubeClientSet.CoreV1().Endpoints(sks.Namespace).Create(resources.MakePublicEndpoints(sks, srcEps))
		if err != nil {
			logger.Errorw(fmt.Sprint("Error creating K8s Endpoints:", sn), zap.Error(err))
			return err
		}
		logger.Info("Created K8s Endpoints: ", sn)
	} else if err != nil {
		logger.Errorw(fmt.Sprint("Error getting K8s Endpoints:", sn), zap.Error(err))
		return err
	} else if !metav1.IsControlledBy(eps, sks) {
		sks.Status.MarkEndpointsNotOwned("Endpoints", sn)
		return fmt.Errorf("SKS: %s does not own Endpoints: %s", sks.Name, sn)
	}
	want := eps.DeepCopy()
	want.Subsets = srcEps.Subsets

	if !equality.Semantic.DeepEqual(want.Subsets, eps.Subsets) {
		logger.Info("Public K8s Endpoints changed; reconciling: ", sn)
		if _, err = r.KubeClientSet.CoreV1().Endpoints(sks.Namespace).Update(want); err != nil {
			logger.Errorw(fmt.Sprint("Error updating public K8s Endpoints:", sn), zap.Error(err))
			return err
		}
	}
	if foundServingEndpoints {
		sks.Status.MarkEndpointsReady()
	} else {
		logger.Infof("Endpoints %s has no ready endpoints", sn)
		sks.Status.MarkEndpointsNotReady("NoHealthyBackends")
	}
	logger.Debug("Done reconciling public K8s endpoints: ", sn)
	return nil
}

func (r *reconciler) privateService(sks *netv1alpha1.ServerlessService) (*corev1.Service, error) {
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
	return nil, apierrs.NewNotFound(corev1.Resource("Services"), sks.Name)
}

func (r *reconciler) reconcilePrivateService(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)

	selector, err := r.getSelector(sks)
	if err != nil {
		return perrors.Wrap(err, "error retrieving deployment selector spec")
	}

	svc, err := r.privateService(sks)
	if errors.IsNotFound(err) {
		logger.Infof("SKS %s has no private service; creating.", sks.Name)
		sks.Status.MarkEndpointsNotReady("CreatingPrivateService")
		svc = resources.MakePrivateService(sks, selector)
		svc, err = r.KubeClientSet.CoreV1().Services(sks.Namespace).Create(svc)
		if err != nil {
			logger.Errorw("Error creating private K8s Service", zap.Error(err))
			return err
		}
		logger.Info("Created private K8s service: ", svc.Name)
	} else if err != nil {
		logger.Errorw("Error getting K8s Service", zap.Error(err))
		return err
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
				logger.Errorw(fmt.Sprint("Error updating private K8s Service:", svc.Name), zap.Error(err))
				return err
			}
		}
	}

	sks.Status.PrivateServiceName = svc.Name
	logger.Debug("Done reconciling private K8s service", svc.Name)
	return nil
}

func (r *reconciler) getSelector(sks *netv1alpha1.ServerlessService) (map[string]string, error) {
	scale, err := presources.GetScaleResource(sks.Namespace, sks.Spec.ObjectRef, r.psInformerFactory)
	if err != nil {
		return nil, err
	}
	return scale.Spec.Selector.MatchLabels, nil
}
