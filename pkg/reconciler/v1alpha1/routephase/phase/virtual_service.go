/*
Copyright 2018 The Knative Authors.

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

package phase

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/knative/pkg/apis/istio/v1alpha3"
	sharedclientset "github.com/knative/pkg/client/clientset/versioned"
	istiolisters "github.com/knative/pkg/client/listers/istio/v1alpha3"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/tracker"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servingclientset "github.com/knative/serving/pkg/client/clientset/versioned"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	reconcilerv1alpha1 "github.com/knative/serving/pkg/reconciler/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
)

func NewVirtualService(o reconciler.CommonOptions, d *reconcilerv1alpha1.DependencyFactory) *VirtualService {
	return &VirtualService{
		ConfigurationLister:  d.Serving.InformerFactory.Serving().V1alpha1().Configurations().Lister(),
		RevisionLister:       d.Serving.InformerFactory.Serving().V1alpha1().Revisions().Lister(),
		VirtualServiceLister: d.Shared.InformerFactory.Networking().V1alpha3().VirtualServices().Lister(),

		ServingClient: d.Serving.Client,
		SharedClient:  d.Shared.Client,
		Recorder:      o.Recorder,
		Tracker:       o.ObjectTracker,
	}
}

type VirtualService struct {
	ConfigurationLister  servinglisters.ConfigurationLister
	RevisionLister       servinglisters.RevisionLister
	VirtualServiceLister istiolisters.VirtualServiceLister

	ServingClient servingclientset.Interface
	SharedClient  sharedclientset.Interface

	Recorder record.EventRecorder
	Tracker  tracker.Interface
}

func (p *VirtualService) Triggers() []reconciler.Trigger {
	return []reconciler.Trigger{
		{
			ObjectKind:  v1alpha3.SchemeGroupVersion.WithKind("VirtualService"),
			OwnerKind:   v1alpha1.SchemeGroupVersion.WithKind("Route"),
			EnqueueType: reconciler.EnqueueOwner,
		}, {
			ObjectKind:  v1alpha1.SchemeGroupVersion.WithKind("Configuration"),
			EnqueueType: reconciler.EnqueueTracker,
		}, {
			ObjectKind:  v1alpha1.SchemeGroupVersion.WithKind("Revision"),
			EnqueueType: reconciler.EnqueueTracker,
		},
	}
}

func (p *VirtualService) Reconcile(ctx context.Context, route *v1alpha1.Route) (v1alpha1.RouteStatus, error) {
	var status v1alpha1.RouteStatus

	logger := logging.FromContext(ctx)
	logger.Infof("Reconciling route - virtual service")

	t, err := traffic.BuildTrafficConfiguration(p.ConfigurationLister, p.RevisionLister, route)

	if t != nil {
		// Tell our trackers to reconcile Route whenever the things referred to by our
		// Traffic stanza change.
		for _, configuration := range t.Configurations {
			if err := p.Tracker.Track(objectRef(configuration), route); err != nil {
				return status, err
			}
		}
		for _, revision := range t.Revisions {
			if revision.Status.IsActivationRequired() {
				logger.Infof("Revision %s/%s is inactive", revision.Namespace, revision.Name)
			}
			if err := p.Tracker.Track(objectRef(revision), route); err != nil {
				return status, err
			}
		}
	}

	badTarget, isTargetError := err.(traffic.TargetError)
	if err != nil && !isTargetError {
		// An error that's not due to missing traffic target should
		// make us fail fast.
		status.MarkUnknownTrafficError(err.Error())
		return status, err
	}

	if badTarget != nil && isTargetError {
		badTarget.MarkBadTrafficTarget(&status)

		// Traffic targets aren't ready, no need to configure Route.
		return status, nil
	}
	logger.Info("All referred targets are routable.  Creating Istio VirtualService.")

	domain := routeDomain(ctx, route)
	desired := resources.MakeVirtualService2(domain, route, t)
	if err := p.reconcileService(logger, route, desired); err != nil {
		return status, err
	}
	logger.Info("VirtualService created, marking AllTrafficAssigned with traffic information.")
	status.Traffic = t.GetRevisionTrafficTargets()
	status.MarkTrafficAssigned()
	return status, nil
}

func (p *VirtualService) reconcileService(
	logger *zap.SugaredLogger,
	route *v1alpha1.Route,
	desiredVirtualService *v1alpha3.VirtualService,
) error {

	ns := desiredVirtualService.Namespace
	name := desiredVirtualService.Name

	virtualService, err := p.VirtualServiceLister.VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		virtualService, err = p.SharedClient.NetworkingV1alpha3().VirtualServices(ns).Create(desiredVirtualService)
		if err != nil {
			logger.Error("Failed to create VirtualService", zap.Error(err))
			p.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %q: %v", name, err)
			return err
		}
		p.Recorder.Eventf(route, corev1.EventTypeNormal, "Created",
			"Created VirtualService %q", desiredVirtualService.Name)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(virtualService.Spec, desiredVirtualService.Spec) {
		virtualService.Spec = desiredVirtualService.Spec
		virtualService, err = p.SharedClient.NetworkingV1alpha3().VirtualServices(ns).Update(virtualService)
		if err != nil {
			logger.Error("Failed to update VirtualService", zap.Error(err))
			return err
		}
	}

	// TODO(mattmoor): This is where we'd look at the state of the VirtualService and
	// reflect any necessary state into the Route.
	return err
}

type accessor interface {
	GroupVersionKind() schema.GroupVersionKind
	GetNamespace() string
	GetName() string
}

func objectRef(a accessor) corev1.ObjectReference {
	gvk := a.GroupVersionKind()
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	return corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  a.GetNamespace(),
		Name:       a.GetName(),
	}
}
