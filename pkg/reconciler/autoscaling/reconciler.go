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

package autoscaling

import (
	"context"
	"fmt"
	"reflect"

	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/autoscaling"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	nv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	nlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/resources"
	anames "knative.dev/serving/pkg/reconciler/autoscaling/resources/names"
	resourceutil "knative.dev/serving/pkg/resources"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// Base implements the core controller logic for autoscaling, given a Reconciler.
type Base struct {
	*reconciler.Base
	PALister          listers.PodAutoscalerLister
	ServiceLister     corev1listers.ServiceLister
	SKSLister         nlisters.ServerlessServiceLister
	MetricLister      listers.MetricLister
	ConfigStore       reconciler.ConfigStore
	PSInformerFactory duck.InformerFactory
}

// ReconcileSKS reconciles a ServerlessService based on the given PodAutoscaler.
func (c *Base) ReconcileSKS(ctx context.Context, pa *pav1alpha1.PodAutoscaler, mode nv1alpha1.ServerlessServiceOperationMode) (*nv1alpha1.ServerlessService, error) {
	logger := logging.FromContext(ctx)

	sksName := anames.SKS(pa.Name)
	sks, err := c.SKSLister.ServerlessServices(pa.Namespace).Get(sksName)
	if errors.IsNotFound(err) {
		logger.Info("SKS does not exist; creating.")
		sks = resources.MakeSKS(pa, mode)
		_, err = c.ServingClientSet.NetworkingV1alpha1().ServerlessServices(sks.Namespace).Create(sks)
		if err != nil {
			return nil, fmt.Errorf("error creating SKS %s: %w", sksName, err)
		}
		logger.Info("Created SKS")
	} else if err != nil {
		return nil, fmt.Errorf("error getting SKS %s: %w", sksName, err)
	} else if !metav1.IsControlledBy(sks, pa) {
		pa.Status.MarkResourceNotOwned("ServerlessService", sksName)
		return nil, fmt.Errorf("PA: %s does not own SKS: %s", pa.Name, sksName)
	} else {
		tmpl := resources.MakeSKS(pa, mode)
		if !equality.Semantic.DeepEqual(tmpl.Spec, sks.Spec) {
			want := sks.DeepCopy()
			want.Spec = tmpl.Spec
			logger.Infof("SKS %s changed; reconciling, want mode: %v", sksName, want.Spec.Mode)
			if sks, err = c.ServingClientSet.NetworkingV1alpha1().ServerlessServices(sks.Namespace).Update(want); err != nil {
				return nil, fmt.Errorf("error updating SKS %s: %w", sksName, err)
			}
		}
	}
	logger.Debug("Done reconciling SKS", sksName)
	return sks, nil
}

func (c *Base) metricService(pa *pav1alpha1.PodAutoscaler) (*corev1.Service, error) {
	svcs, err := c.ServiceLister.Services(pa.Namespace).List(labels.SelectorFromSet(map[string]string{
		autoscaling.KPALabelKey:   pa.Name,
		networking.ServiceTypeKey: string(networking.ServiceTypeMetrics),
	}))
	if err != nil {
		return nil, err
	}
	var ret *corev1.Service
	for _, s := range svcs {
		// TODO(vagababov): determine if this is better to be in the ownership check.
		// Found a match or we had nothing set up, then pick any of them, to reduce churn.
		if s.Name == pa.Status.MetricsServiceName {
			ret = s
			continue
		}
		// If it's not the metrics service recorded in status,
		// but we control it then it is a duplicate and should be deleted.
		if metav1.IsControlledBy(s, pa) {
			c.KubeClientSet.CoreV1().Services(pa.Namespace).Delete(s.Name, &metav1.DeleteOptions{})
		}
	}
	if ret == nil {
		return nil, errors.NewNotFound(corev1.Resource("Services"), pa.Name)
	}
	return ret, nil
}

// ReconcileMetricsService reconciles a metrics service for the given PodAutoscaler.
func (c *Base) ReconcileMetricsService(ctx context.Context, pa *pav1alpha1.PodAutoscaler) (string, error) {
	logger := logging.FromContext(ctx)

	scale, err := resourceutil.GetScaleResource(pa.Namespace, pa.Spec.ScaleTargetRef, c.PSInformerFactory)
	if err != nil {
		return "", fmt.Errorf("error retrieving scale: %w", err)
	}
	selector := scale.Spec.Selector.MatchLabels
	logger.Debugf("PA's selector: %v", selector)

	svc, err := c.metricService(pa)
	if errors.IsNotFound(err) {
		logger.Info("Metrics K8s service for PA does not exist; creating.")
		svc = resources.MakeMetricsService(pa, selector)
		svc, err = c.KubeClientSet.CoreV1().Services(pa.Namespace).Create(svc)
		if err != nil {
			return "", fmt.Errorf("error creating metrics K8s service for %s/%s: %w", pa.Namespace, pa.Name, err)
		}
		logger.Info("Created K8s service:", svc.Name)
	} else if err != nil {
		return "", fmt.Errorf("error getting metrics K8s service: %w", err)
	} else if !metav1.IsControlledBy(svc, pa) {
		pa.Status.MarkResourceNotOwned("Service", svc.Name)
		return "", fmt.Errorf("PA: %s does not own Service: %s", pa.Name, svc.Name)
	} else {
		tmpl := resources.MakeMetricsService(pa, selector)
		want := svc.DeepCopy()
		want.Spec.Ports = tmpl.Spec.Ports
		want.Spec.Selector = tmpl.Spec.Selector

		if !equality.Semantic.DeepEqual(want.Spec, svc.Spec) {
			logger.Info("Metrics K8s Service changed; reconciling:", svc.Name)
			if _, err = c.KubeClientSet.CoreV1().Services(pa.Namespace).Update(want); err != nil {
				return "", fmt.Errorf("error updating K8s Service %s: %w", svc.Name, err)
			}
		}
	}
	pa.Status.MetricsServiceName = svc.Name
	logger.Debug("Done reconciling metrics K8s service: ", svc.Name)
	return svc.Name, nil
}

// ReconcileMetric reconciles a metric instance out of the given PodAutoscaler to control metric collection.
func (c *Base) ReconcileMetric(ctx context.Context, pa *pav1alpha1.PodAutoscaler, metricSN string) error {
	desiredMetric := resources.MakeMetric(ctx, pa, metricSN, config.FromContext(ctx).Autoscaler)
	metric, err := c.MetricLister.Metrics(desiredMetric.Namespace).Get(desiredMetric.Name)
	if errors.IsNotFound(err) {
		_, err = c.ServingClientSet.AutoscalingV1alpha1().Metrics(desiredMetric.Namespace).Create(desiredMetric)
		if err != nil {
			return fmt.Errorf("error creating metric: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("error fetching metric: %w", err)
	} else if !metav1.IsControlledBy(metric, pa) {
		pa.Status.MarkResourceNotOwned("Metric", desiredMetric.Name)
		return fmt.Errorf("PA: %s does not own Metric: %s", pa.Name, desiredMetric.Name)
	} else {
		if !equality.Semantic.DeepEqual(desiredMetric.Spec, metric.Spec) {
			want := metric.DeepCopy()
			want.Spec = desiredMetric.Spec
			if _, err = c.ServingClientSet.AutoscalingV1alpha1().Metrics(desiredMetric.Namespace).Update(want); err != nil {
				return fmt.Errorf("error updating metric: %w", err)
			}
		}
	}

	return nil
}

// UpdateStatus updates the status of the given PodAutoscaler.
func (c *Base) UpdateStatus(desired *pav1alpha1.PodAutoscaler) (*pav1alpha1.PodAutoscaler, error) {
	pa, err := c.PALister.PodAutoscalers(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(pa.Status, desired.Status) {
		return pa, nil
	}
	// Don't modify the informers copy
	existing := pa.DeepCopy()
	existing.Status = desired.Status

	return c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(pa.Namespace).UpdateStatus(existing)
}
