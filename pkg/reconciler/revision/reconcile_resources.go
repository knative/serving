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

	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/logging/logkey"
	kpav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/revision/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/revision/resources/names"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Reconciler) reconcileDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	ns := rev.Namespace
	deploymentName := resourcenames.Deployment(rev)
	logger := logging.FromContext(ctx).With(zap.String(logkey.Deployment, deploymentName))

	deployment, err := c.deploymentLister.Deployments(ns).Get(deploymentName)
	if apierrs.IsNotFound(err) {
		// Deployment does not exist. Create it.
		rev.Status.MarkDeploying("Deploying")
		deployment, err = c.createDeployment(ctx, rev)
		if err != nil {
			logger.Errorf("Error creating deployment %q: %v", deploymentName, err)
			return err
		}
		logger.Infof("Created deployment %q", deploymentName)
	} else if err != nil {
		logger.Errorf("Error reconciling deployment %q: %v", deploymentName, err)
		return err
	} else if !metav1.IsControlledBy(deployment, rev) {
		// Surface an error in the revision's status, and return an error.
		rev.Status.MarkResourceNotOwned("Deployment", deploymentName)
		return fmt.Errorf("revision: %q does not own Deployment: %q", rev.Name, deploymentName)
	} else {
		// The deployment exists, but make sure that it has the shape that we expect.
		deployment, err = c.checkAndUpdateDeployment(ctx, rev, deployment)
		if err != nil {
			logger.Errorf("Error updating deployment %q: %v", deploymentName, err)
			return err
		}
	}

	// Now that we have a Deployment, determine whether there is any relevant
	// status to surface in the Revision.
	if hasDeploymentTimedOut(deployment) && !rev.Status.IsActivationRequired() {
		rev.Status.MarkProgressDeadlineExceeded(fmt.Sprintf(
			"Unable to create pods for more than %d seconds.", resources.ProgressDeadlineSeconds))
		c.Recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded",
			"Revision %s not ready due to Deployment timeout", rev.Name)
	}

	return nil
}

func (c *Reconciler) reconcileImageCache(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)

	ns := rev.Namespace
	imageName := resourcenames.ImageCache(rev)
	_, getImageCacheErr := c.imageLister.Images(ns).Get(imageName)
	if apierrs.IsNotFound(getImageCacheErr) {
		_, err := c.createImageCache(ctx, rev)
		if err != nil {
			logger.Errorf("Error creating image cache %q: %v", imageName, err)
			return err
		}
		logger.Infof("Created image cache %q", imageName)
	} else if getImageCacheErr != nil {
		logger.Errorf("Error reconciling image cache %q: %v", imageName, getImageCacheErr)
		return getImageCacheErr
	}

	return nil
}

func (c *Reconciler) reconcileKPA(ctx context.Context, rev *v1alpha1.Revision) error {
	ns := rev.Namespace
	kpaName := resourcenames.KPA(rev)
	logger := logging.FromContext(ctx)
	logger.Info("Reconciling KPA:", kpaName)

	kpa, err := c.podAutoscalerLister.PodAutoscalers(ns).Get(kpaName)
	if apierrs.IsNotFound(err) {
		// KPA does not exist. Create it.
		kpa, err = c.createKPA(ctx, rev)
		if err != nil {
			logger.Errorf("Error creating KPA %s: %v", kpaName, err)
			return err
		}
		logger.Info("Created KPA:", kpaName)
	} else if err != nil {
		logger.Errorf("Error reconciling kpa %s: %v", kpaName, err)
		return err
	} else if !metav1.IsControlledBy(kpa, rev) {
		// Surface an error in the revision's status, and return an error.
		rev.Status.MarkResourceNotOwned("PodAutoscaler", kpaName)
		return fmt.Errorf("revision: %q does not own PodAutoscaler: %q", rev.Name, kpaName)
	}

	// Perhaps tha KPA spec changed underneath ourselves?
	// TODO(vagababov): required for #1997. Should be removed in 0.7,
	// to fix the protocol type when it's unset.
	tmpl := resources.MakeKPA(rev)
	if !equality.Semantic.DeepEqual(tmpl.Spec, kpa.Spec) {
		logger.Infof("KPA %s needs reconciliation", kpa.Name)

		want := kpa.DeepCopy()
		want.Spec = tmpl.Spec
		if kpa, err = c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Update(want); err != nil {
			return err
		}
		// This change will trigger KPA -> SKS -> K8s service change;
		// and those after reconciliation will back progpagate here.
		rev.Status.MarkDeploying("Updating")
	}

	// Propagate the service name from the PA.
	rev.Status.ServiceName = kpa.Status.ServiceName

	// Reflect the KPA status in our own.
	cond := kpa.Status.GetCondition(kpav1alpha1.PodAutoscalerConditionActive)
	switch {
	case cond == nil:
		rev.Status.MarkActivating("Deploying", "")
		// If not ready => SKS did not report a service name, we can reliably use.
	case cond.Status == corev1.ConditionUnknown:
		rev.Status.MarkActivating(cond.Reason, cond.Message)
	case cond.Status == corev1.ConditionFalse:
		rev.Status.MarkInactive(cond.Reason, cond.Message)
	case cond.Status == corev1.ConditionTrue:
		rev.Status.MarkActive()
	}

	cond = kpa.Status.GetCondition(kpav1alpha1.PodAutoscalerConditionReady)
	if cond != nil {
		switch {
		case cond.Status == corev1.ConditionTrue:
			rev.Status.MarkContainerHealthy()
			rev.Status.MarkResourcesAvailable()
		case cond.Status == corev1.ConditionFalse:
			rev.Status.MarkContainerUnhealthy(cond.Reason, cond.Message)
		}
	}

	return nil
}

func hasDeploymentTimedOut(deployment *appsv1.Deployment) bool {
	// as per https://kubernetes.io/docs/concepts/workloads/controllers/deployment
	for _, cond := range deployment.Status.Conditions {
		// Look for Deployment with status False
		if cond.Status != corev1.ConditionFalse {
			continue
		}
		// with Type Progressing and Reason Timeout
		// TODO(arvtiwar): hard coding "ProgressDeadlineExceeded" to avoid import kubernetes/kubernetes
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			return true
		}
	}
	return false
}
