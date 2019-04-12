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

package serverlessservice

import (
	"context"
	"fmt"
	"reflect"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	perrors "github.com/pkg/errors"

	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/networking"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	rbase "github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/serverlessservice/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/serverlessservice/resources/names"
	presources "github.com/knative/serving/pkg/resources"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "serverlessservice-controller"
	reconcilerName      = "ServerlessServices"
	activatorService    = "activator-service"
)

// reconciler implements controller.Reconciler for Service resources.
type reconciler struct {
	*rbase.Base

	// listers index properties about resources
	sksLister       listers.ServerlessServiceLister
	serviceLister   corev1listers.ServiceLister
	endpointsLister corev1listers.EndpointsLister
}

// NewController initializes the controller and is called by the generated code.
// Registers eventhandlers to enqueue events.
func NewController(
	opt rbase.Options,
	sksInformer informers.ServerlessServiceInformer,
	serviceInformer corev1informers.ServiceInformer,
	endpointsInformer corev1informers.EndpointsInformer,
) *controller.Impl {
	c := &reconciler{
		Base:            rbase.NewBase(opt, controllerAgentName),
		endpointsLister: endpointsInformer.Lister(),
		serviceLister:   serviceInformer.Lister(),
		sksLister:       sksInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, reconcilerName, rbase.MustNewStatsReporter(reconcilerName, c.Logger))

	c.Logger.Info("Setting up event handlers")

	// Watch all the SKS objects.
	sksInformer.Informer().AddEventHandler(rbase.Handler(impl.Enqueue))

	// Watch all the endpoints that we have attached our label to.
	endpointsInformer.Informer().AddEventHandler(
		rbase.Handler(impl.EnqueueLabelOfNamespaceScopedResource("" /*any namespace*/, networking.SKSLabelKey)))

	// Watch all the services that we have created.
	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(netv1alpha1.SchemeGroupVersion.WithKind("ServerlessService")),
		Handler:    rbase.Handler(impl.EnqueueControllerOf),
	})

	// Watch activator-service endpoints.
	grCb := func(obj interface{}) {
		// Since changes in the Activar Service endpoints affect all the SKS objects,
		// do a global resync.
		c.Logger.Info("Doing a global resync due to activator endpoint changes")
		impl.GlobalResync(sksInformer.Informer())
	}
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		// Accept only ActivatorService K8s service objects.
		FilterFunc: rbase.ChainFilterFuncs(
			rbase.NamespaceFilterFunc(system.Namespace()),
			rbase.NameFilterFunc(activatorService)),
		Handler: rbase.Handler(grCb),
	})

	return impl
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
	err = r.reconcile(ctx, sks)
	if err != nil {
		r.Recorder.Eventf(sks, corev1.EventTypeWarning, "UpdateFailed", "InternalError: %v", err.Error())
		return err
	}
	if !equality.Semantic.DeepEqual(sks.Status, original.Status) {
		// Only update status if it changed.
		if _, err = r.updateStatus(sks); err != nil {
			r.Recorder.Eventf(sks, corev1.EventTypeWarning, "UpdateFailed", "Failed to update status: %v", err)
			return err
		}
		r.Recorder.Eventf(sks, corev1.EventTypeNormal, "Updated", "Successfully updated ServerlessService %q", key)
	}
	return nil
}

func (r *reconciler) reconcile(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)
	// Don't reconcile if we're being deleted.
	if sks.GetDeletionTimestamp() != nil {
		return nil
	}

	sks.SetDefaults(ctx)
	sks.Status.InitializeConditions()

	// TODO(#1997): implement: public service, proxy mode, activator probing and positive handoff.
	for i, fn := range []func(context.Context, *netv1alpha1.ServerlessService) error{
		r.reconcilePrivateService, // First make sure our data source is setup.
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
	sn := names.PublicService(sks)
	srv, err := r.serviceLister.Services(sks.Namespace).Get(sn)
	if errors.IsNotFound(err) {
		logger.Infof("K8s service %q does not exist; creating.", sn)
		// We've just created the service, so it has no endpoints.
		sks.Status.MarkEndpointsNotReady("CreatingPublicService")
		srv = resources.MakePublicService(sks)
		_, err := r.KubeClientSet.CoreV1().Services(sks.Namespace).Create(srv)
		if err != nil {
			logger.Errorw(fmt.Sprintf("Error creating K8s Service %s: ", sn), zap.Error(err))
			return err
		}
		logger.Infof("Created K8s service: %q", sn)
	} else if err != nil {
		logger.Errorw(fmt.Sprintf("Error getting K8s Service %s: ", sn), zap.Error(err))
		return err
	} else if !metav1.IsControlledBy(srv, sks) {
		sks.Status.MarkEndpointsNotOwned("Service", sn)
		return fmt.Errorf("SKS: %q does not own Service: %q", sks.Name, sn)
	} else {
		tmpl := resources.MakePublicService(sks)
		want := srv.DeepCopy()
		want.Spec.Ports = tmpl.Spec.Ports
		want.Spec.Selector = nil

		sks.Status.MarkEndpointsNotReady("UpdatingPublicService")
		if !equality.Semantic.DeepEqual(want.Spec, srv.Spec) {
			logger.Infof("Public K8s Service changed; reconciling: %s", sn)
			if _, err = r.KubeClientSet.CoreV1().Services(sks.Namespace).Update(want); err != nil {
				logger.Errorw(fmt.Sprintf("Error updating public K8s Service %s: ", sn), zap.Error(err))
				return err
			}
		}
	}
	sks.Status.ServiceName = sn
	logger.Debugf("Done reconciling public K8s service %s", sn)
	return nil
}

func (r *reconciler) reconcilePublicEndpoints(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)

	// Service and Endpoints have the same name.
	// Get private endpoints first, since if they are not available there's nothing we can do.
	psn := names.PrivateService(sks)
	srcEps, err := r.endpointsLister.Endpoints(sks.Namespace).Get(psn)
	if err != nil {
		logger.Error(fmt.Sprintf("Error obtaining private service endpoints: %s", psn), zap.Error(err))
		return err
	}
	logger.Debugf("Public endpoints: %s", spew.Sprint(srcEps))

	sn := names.PublicService(sks)
	eps, err := r.endpointsLister.Endpoints(sks.Namespace).Get(sn)

	if errors.IsNotFound(err) {
		logger.Infof("K8s endpoints %q does not exist; creating.", sn)
		sks.Status.MarkEndpointsNotReady("CreatingPublicEndpoints")
		eps, err = r.KubeClientSet.CoreV1().Endpoints(sks.Namespace).Create(resources.MakePublicEndpoints(sks, srcEps))
		if err != nil {
			logger.Errorw(fmt.Sprintf("Error creating K8s Endpoints %s: ", sn), zap.Error(err))
			return err
		}
		logger.Infof("Created K8s Endpoints: %q", sn)
	} else if err != nil {
		logger.Errorw(fmt.Sprintf("Error getting K8s Endpoints %s: ", sn), zap.Error(err))
		return err
	} else if !metav1.IsControlledBy(eps, sks) {
		sks.Status.MarkEndpointsNotOwned("Endpoints", sn)
		return fmt.Errorf("SKS: %q does not own Endpoints: %q", sks.Name, sn)
	} else {
		want := eps.DeepCopy()
		want.Subsets = srcEps.Subsets

		if !equality.Semantic.DeepEqual(want.Subsets, eps.Subsets) {
			logger.Infof("Public K8s Endpoints changed; reconciling: %s", sn)
			if _, err = r.KubeClientSet.CoreV1().Endpoints(sks.Namespace).Update(want); err != nil {
				logger.Errorw(fmt.Sprintf("Error updating public K8s Endpoints %s: ", sn), zap.Error(err))
				return err
			}
		}
	}
	if r := presources.ReadyAddressCount(eps); r > 0 {
		sks.Status.MarkEndpointsReady()
	} else {
		logger.Info("Endpoints %s/%s has no ready endpoints")
		sks.Status.MarkEndpointsNotReady("NoHealthyBackends")
	}
	logger.Debugf("Done reconciling public K8s endpoints %s", sn)
	return nil
}

func (r *reconciler) reconcilePrivateService(ctx context.Context, sks *netv1alpha1.ServerlessService) error {
	logger := logging.FromContext(ctx)
	sn := names.PrivateService(sks)
	svc, err := r.serviceLister.Services(sks.Namespace).Get(sn)
	if errors.IsNotFound(err) {
		logger.Infof("K8s service %s does not exist; creating.", sn)
		sks.Status.MarkEndpointsNotReady("CreatingPrivateService")
		svc = resources.MakePrivateService(sks)
		_, err := r.KubeClientSet.CoreV1().Services(sks.Namespace).Create(svc)
		if err != nil {
			logger.Errorw(fmt.Sprint("Error creating K8s Service:", sn), zap.Error(err))
			return err
		}
		logger.Infof("Created K8s service: %s", sn)
	} else if err != nil {
		logger.Errorw(fmt.Sprint("Error getting K8s Service:", sn), zap.Error(err))
		return err
	} else if !metav1.IsControlledBy(svc, sks) {
		sks.Status.MarkEndpointsNotOwned("Service", sn)
		return fmt.Errorf("SKS: %s does not own Service: %s", sks.Name, sn)
	}
	tmpl := resources.MakePrivateService(sks)
	want := svc.DeepCopy()
	// Our controller manages only part of spec, so set the fields we own.
	want.Spec.Ports = tmpl.Spec.Ports
	want.Spec.Selector = tmpl.Spec.Selector

	if !equality.Semantic.DeepEqual(svc.Spec, want.Spec) {
		sks.Status.MarkEndpointsNotReady("UpdatingPrivateService")
		logger.Info("Private K8s Service changed; reconciling:", sn)
		if _, err = r.KubeClientSet.CoreV1().Services(sks.Namespace).Update(want); err != nil {
			logger.Errorw(fmt.Sprint("Error updating private K8s Service:", sn), zap.Error(err))
			return err
		}
	}

	// TODO(#1997): temporarily we have one service since istio cannot handle our load.
	// So they are smudged together. In the end this has to go.
	eps, err := presources.FetchReadyAddressCount(r.endpointsLister, sks.Namespace, sn)
	switch {
	case err != nil:
		return perrors.Wrapf(err, "error fetching endpoints %s/%s", sks.Namespace, sn)
	case eps > 0:
		logger.Infof("Endpoints %s/%s has %d ready endpoints", sks.Namespace, sn, eps)
		sks.Status.MarkEndpointsReady()
	default:
		logger.Infof("Endpoints %s/%s has no ready endpoints", sks.Namespace, sn)
		sks.Status.MarkEndpointsNotReady("NoHealthyBackends")
	}
	sks.Status.ServiceName = sn
	// End TODO.

	sks.Status.PrivateServiceName = sn
	logger.Debug("Done reconciling private K8s service", sn)
	return nil
}
