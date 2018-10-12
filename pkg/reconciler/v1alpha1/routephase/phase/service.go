/*
Copyright 2018 The Knative Authors

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

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	reconcilerv1alpha1 "github.com/knative/serving/pkg/reconciler/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources/names"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
)

func NewK8sService(o reconciler.CommonOptions, d *reconcilerv1alpha1.DependencyFactory) *K8sService {
	return &K8sService{
		ServiceLister: d.Kubernetes.InformerFactory.Core().V1().Services().Lister(),
		KubeClientSet: d.Kubernetes.Client,
		Recorder:      o.Recorder,
	}
}

type K8sService struct {
	ServiceLister corev1listers.ServiceLister
	KubeClientSet kubernetes.Interface
	Recorder      record.EventRecorder
}

func (p *K8sService) Triggers() []reconciler.Trigger {
	return []reconciler.Trigger{{
		ObjectKind:  corev1.SchemeGroupVersion.WithKind("Service"),
		OwnerKind:   v1alpha1.SchemeGroupVersion.WithKind("Route"),
		EnqueueType: reconciler.EnqueueOwner,
	}}
}

func (p *K8sService) Reconcile(ctx context.Context, route *v1alpha1.Route) (status v1alpha1.RouteStatus, err error) {

	logger := logging.FromContext(ctx)
	logger.Infof("Reconciling route - kubernetes service")

	ns := route.Namespace
	name := names.K8sService(route)

	service, err := p.ServiceLister.Services(ns).Get(name)

	if apierrs.IsNotFound(err) {
		err = p.create(logger, route)
	} else if err == nil {
		err = p.update(logger, route, service)
	}

	if err != nil {
		return
	}

	// Update the information that makes us Targetable.
	status.DomainInternal = names.K8sServiceFullname(route)
	status.Targetable = &duckv1alpha1.Targetable{
		DomainInternal: names.K8sServiceFullname(route),
	}

	// TODO(mattmoor): This is where we'd look at the state of the Service and
	// reflect any necessary state into the Route.
	return
}

func (p *K8sService) create(logger *zap.SugaredLogger, route *v1alpha1.Route) error {
	service := resources.MakeK8sService(route)
	name := names.K8sService(route)

	_, err := p.KubeClientSet.CoreV1().Services(route.Namespace).Create(service)
	if err != nil {
		logger.Error("Failed to create service", zap.Error(err))
		p.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create service %q: %v", name, err)
		return err
	}

	logger.Infof("Created service %s", name)
	p.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created service %q", name)
	return nil
}

func (p *K8sService) update(logger *zap.SugaredLogger, route *v1alpha1.Route, service *corev1.Service) error {
	// Make sure that the service has the proper specification
	desiredService := resources.MakeK8sService(route)

	// Preserve the ClusterIP field in the Service's Spec, if it has been set.
	desiredService.Spec.ClusterIP = service.Spec.ClusterIP

	if equality.Semantic.DeepEqual(service.Spec, desiredService.Spec) {
		return nil
	}

	service.Spec = desiredService.Spec
	_, err := p.KubeClientSet.CoreV1().
		Services(route.Namespace).
		Update(service)

	if err != nil {
		logger.Error("Failed to update service", zap.Error(err))
	} else {
		logger.Infof("Updated service %s", desiredService.Name)
	}

	return err
}
