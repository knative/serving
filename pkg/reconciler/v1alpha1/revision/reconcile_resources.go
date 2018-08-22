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

package revision

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"

	commonlogkey "github.com/knative/pkg/logging/logkey"
	kpav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	serviceTimeoutDuration = 5 * time.Minute
)

func (c *Reconciler) reconcileDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	ns := rev.Namespace
	deploymentName := resourcenames.Deployment(rev)
	logger := logging.FromContext(ctx).With(zap.String(commonlogkey.Deployment, deploymentName))

	deployment, getDepErr := c.deploymentLister.Deployments(ns).Get(deploymentName)
	// When Active or Reserved, deployment should exist and have a particular specification.
	if apierrs.IsNotFound(getDepErr) {
		// Deployment does not exist. Create it.
		rev.Status.MarkDeploying("Deploying")
		var err error
		deployment, err = c.createDeployment(ctx, rev)
		if err != nil {
			logger.Errorf("Error creating deployment %q: %v", deploymentName, err)
			return err
		}
		logger.Infof("Created deployment %q", deploymentName)
	} else if getDepErr != nil {
		logger.Errorf("Error reconciling deployment %q: %v", deploymentName, getDepErr)
		return getDepErr
	} else {
		// Deployment exist. Update the replica count based on the serving state if necessary
		var changed Changed
		var err error
		deployment, changed, err = c.checkAndUpdateDeployment(ctx, rev, deployment)
		if err != nil {
			logger.Errorf("Error updating deployment %q: %v", deploymentName, err)
			return err
		}
		if changed == WasChanged {
			logger.Infof("Updated deployment %q", deploymentName)
			rev.Status.MarkDeploying("Updating")
		}
	}

	// Now that we have a Deployment, determine whether there is any relevant
	// status to surface in the Revision.
	if hasDeploymentTimedOut(deployment) {
		rev.Status.MarkProgressDeadlineExceeded(fmt.Sprintf(
			"Unable to create pods for more than %d seconds.", resources.ProgressDeadlineSeconds))
		c.Recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded",
			"Revision %s not ready due to Deployment timeout", rev.Name)
	}
	return nil
}

func (c *Reconciler) reconcileKPA(ctx context.Context, rev *v1alpha1.Revision) error {
	ns := rev.Namespace
	kpaName := resourcenames.KPA(rev)
	logger := logging.FromContext(ctx).With(zap.String(logkey.KPA, kpaName))

	kpa, getKPAErr := c.kpaLister.PodAutoscalers(ns).Get(kpaName)
	if apierrs.IsNotFound(getKPAErr) {
		// KPA does not exist. Create it.
		var err error
		kpa, err = c.createKPA(ctx, rev)
		if err != nil {
			logger.Errorf("Error creating KPA %q: %v", kpaName, err)
			return err
		}
		logger.Infof("Created kpa %q", kpaName)
	} else if getKPAErr != nil {
		logger.Errorf("Error reconciling kpa %q: %v", kpaName, getKPAErr)
		return getKPAErr
	} else {
		// KPA exists. Update the replica count based on the serving state if necessary
		var err error
		kpa, _, err = c.checkAndUpdateKPA(ctx, rev, kpa)
		if err != nil {
			logger.Errorf("Error updating kpa %q: %v", kpaName, err)
			return err
		}
	}

	// Reflect the KPA status in our own.
	cond := kpa.Status.GetCondition(kpav1alpha1.PodAutoscalerConditionReady)
	switch {
	case cond == nil:
		// TODO(mattmoor): rev.Status.MarkActivating("Deploying", "")
	case cond.Status == corev1.ConditionUnknown:
		// TODO(mattmoor): rev.Status.MarkActivating(cond.Reason, cond.Message)
	case cond.Status == corev1.ConditionFalse:
		rev.Status.MarkInactive(cond.Message)
	case cond.Status == corev1.ConditionTrue:
		// TODO(mattmoor): rev.Status.MarkActive()
	}
	return nil
}

func (c *Reconciler) reconcileService(ctx context.Context, rev *v1alpha1.Revision) error {
	ns := rev.Namespace
	serviceName := resourcenames.K8sService(rev)
	logger := logging.FromContext(ctx).With(zap.String(commonlogkey.KubernetesService, serviceName))

	rev.Status.ServiceName = serviceName

	service, err := c.serviceLister.Services(ns).Get(serviceName)
	// When Active, the Service should exist and have a particular specification.
	if apierrs.IsNotFound(err) {
		// If it does not exist, then create it.
		rev.Status.MarkDeploying("Deploying")
		service, err = c.createService(ctx, rev, resources.MakeK8sService)
		if err != nil {
			logger.Errorf("Error creating Service %q: %v", serviceName, err)
			return err
		}
		logger.Infof("Created Service %q", serviceName)
	} else if err != nil {
		logger.Errorf("Error reconciling Active Service %q: %v", serviceName, err)
		return err
	} else {
		// If it exists, then make sure if looks as we expect.
		// It may change if a user edits things around our controller, which we
		// should not allow, or if our expectations of how the service should look
		// changes (e.g. we update our controller with new sidecars).
		var changed Changed
		service, changed, err = c.checkAndUpdateService(ctx, rev, resources.MakeK8sService, service)
		if err != nil {
			logger.Errorf("Error updating Service %q: %v", serviceName, err)
			return err
		}
		if changed == WasChanged {
			logger.Infof("Updated Service %q", serviceName)
			rev.Status.MarkDeploying("Updating")
		}
	}

	// We cannot determine readiness from the Service directly.  Instead, we look up
	// the backing Endpoints resource and check it for healthy pods.  The name of the
	// Endpoints resource matches the Service it backs.
	endpoints, err := c.endpointsLister.Endpoints(ns).Get(serviceName)
	if apierrs.IsNotFound(err) {
		// If it isn't found, then we need to wait for the Service controller to
		// create it.
		logger.Infof("Endpoints not created yet %q", serviceName)
		rev.Status.MarkDeploying("Deploying")
		return nil
	} else if err != nil {
		logger.Errorf("Error checking Active Endpoints %q: %v", serviceName, err)
		return err
	}

	// If the endpoints resource indicates that the Service it sits in front of is ready,
	// then surface this in our Revision status as resources available (pods were scheduled)
	// and container healthy (endpoints should be gated by any provided readiness checks).
	if getIsServiceReady(endpoints) {
		rev.Status.MarkResourcesAvailable()
		rev.Status.MarkContainerHealthy()
		// TODO(mattmoor): How to ensure this only fires once?
		c.Recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady",
			"Revision becomes ready upon endpoint %q becoming ready", serviceName)
	} else {
		// If the endpoints is NOT ready, then check whether it is taking unreasonably
		// long to become ready and if so mark our revision as having timed out waiting
		// for the Service to become ready.
		revisionAge := time.Now().Sub(getRevisionLastTransitionTime(rev))
		if revisionAge >= serviceTimeoutDuration {
			rev.Status.MarkServiceTimeout()
			// TODO(mattmoor): How to ensure this only fires once?
			c.Recorder.Eventf(rev, corev1.EventTypeWarning, "RevisionFailed",
				"Revision did not become ready due to endpoint %q", serviceName)
		}
	}
	return nil
}

func (c *Reconciler) reconcileFluentdConfigMap(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	if !c.getObservabilityConfig().EnableVarLogCollection {
		return nil
	}
	ns := rev.Namespace
	name := resourcenames.FluentdConfigMap(rev)

	configMap, err := c.configMapLister.ConfigMaps(ns).Get(name)
	if apierrs.IsNotFound(err) {
		// ConfigMap doesn't exist, going to create it
		desiredConfigMap := resources.MakeFluentdConfigMap(rev, c.getObservabilityConfig())
		configMap, err = c.KubeClientSet.CoreV1().ConfigMaps(ns).Create(desiredConfigMap)
		if err != nil {
			logger.Error("Error creating fluentd configmap", zap.Error(err))
			return err
		}
		logger.Infof("Created fluentd configmap: %q", name)
	} else if err != nil {
		logger.Errorf("configmaps.Get for %q failed: %s", name, err)
		return err
	} else {
		desiredConfigMap := resources.MakeFluentdConfigMap(rev, c.getObservabilityConfig())
		if !equality.Semantic.DeepEqual(configMap.Data, desiredConfigMap.Data) {
			logger.Infof("Reconciling fluentd configmap diff (-desired, +observed): %v",
				cmp.Diff(desiredConfigMap.Data, configMap.Data))
			configMap.Data = desiredConfigMap.Data
			configMap, err = c.KubeClientSet.CoreV1().ConfigMaps(ns).Update(desiredConfigMap)
			if err != nil {
				logger.Error("Error updating fluentd configmap", zap.Error(err))
				return err
			}
		}
	}
	return nil
}

// TODO(#1876): Move this into the KPA's scope.
func (c *Reconciler) reconcileVPA(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	if !c.getAutoscalerConfig().EnableVPA {
		return nil
	}

	ns := rev.Namespace
	vpaName := resourcenames.VPA(rev)

	// TODO(mattmoor): Switch to informer lister once it can reliably be sunk.
	_, err := c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(ns).Get(vpaName, metav1.GetOptions{})
	// When Active, the VPA should exist and have a particular specification.
	if apierrs.IsNotFound(err) {
		// If it does not exist, then create it.
		_, err = c.createVPA(ctx, rev)
		if err != nil {
			logger.Errorf("Error creating VPA %q: %v", vpaName, err)
			return err
		}
		logger.Infof("Created VPA %q", vpaName)
	} else if err != nil {
		logger.Errorf("Error reconciling Active VPA %q: %v", vpaName, err)
		return err
	} else {
		// TODO(mattmoor): Should we checkAndUpdate the VPA, or would it
		// suffer similar problems to Deployment?
	}

	return nil
}
