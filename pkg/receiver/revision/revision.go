/*
Copyright 2018 Google LLC.

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
	"reflect"

	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	// TODO(mattmoor): Move controller deps into receiver
	"github.com/knative/serving/pkg/controller"
)

// SyncRevision implements revision.Receiver
func (c *Receiver) SyncRevision(rev *v1alpha1.Revision) error {
	logger := loggerWithRevisionInfo(c.Logger, rev.Namespace, rev.Name)
	ctx := logging.WithLogger(context.TODO(), logger)

	if err := c.updateRevisionLoggingURL(rev); err != nil {
		logger.Error("Error updating the revisions logging url", zap.Error(err))
		return err
	}

	if rev.Spec.BuildName != "" {
		if done, failed := isBuildDone(rev); !done {
			if alreadyTracked := c.buildtracker.Track(rev); !alreadyTracked {
				if err := c.markRevisionBuilding(ctx, rev); err != nil {
					logger.Error("Error recording the BuildSucceeded=Unknown condition", zap.Error(err))
					return err
				}
			}
			return nil
		} else {
			// The Build's complete, so stop tracking it.
			c.buildtracker.Untrack(rev)
			if failed {
				return nil
			}
			// If the build didn't fail, proceed to creating K8s resources.
		}
	}

	if _, err := controller.GetOrCreateRevisionNamespace(ctx, rev.Namespace, c.KubeClientSet); err != nil {
		logger.Panic("Failed to create namespace", zap.Error(err))
	}
	logger.Info("Namespace validated to exist, moving on")

	return c.reconcileWithImage(ctx, rev, rev.Namespace)
}

// reconcileWithImage handles enqueued messages that have an image.
func (c *Receiver) reconcileWithImage(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	err := c.reconcileOnceBuilt(ctx, rev, ns)
	if err != nil {
		logging.FromContext(ctx).Error("Reconcile once build failed", zap.Error(err))
	}
	return err
}

// reconcileOnceBuilt handles enqueued messages that have an image.
func (c *Receiver) reconcileOnceBuilt(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	logger := logging.FromContext(ctx)
	accessor, err := meta.Accessor(rev)
	if err != nil {
		logger.Panic("Failed to get metadata", zap.Error(err))
	}

	deletionTimestamp := accessor.GetDeletionTimestamp()
	logger.Infof("Check the deletionTimestamp: %s\n", deletionTimestamp)

	elaNS := controller.GetElaNamespaceName(rev.Namespace)

	if deletionTimestamp == nil && rev.Spec.ServingState == v1alpha1.RevisionServingStateActive {
		logger.Info("Creating or reconciling resources for revision")
		return c.createK8SResources(ctx, rev, elaNS)
	}
	return c.deleteK8SResources(ctx, rev, elaNS)
}

func (c *Receiver) deleteK8SResources(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	logger := logging.FromContext(ctx)
	logger.Info("Deleting the resources for revision")
	err := c.deleteDeployment(ctx, rev, ns)
	if err != nil {
		logger.Error("Failed to delete a deployment", zap.Error(err))
	}
	logger.Info("Deleted deployment")

	err = c.deleteAutoscalerDeployment(ctx, rev)
	if err != nil {
		logger.Error("Failed to delete autoscaler Deployment", zap.Error(err))
	}
	logger.Info("Deleted autoscaler Deployment")

	err = c.deleteAutoscalerService(ctx, rev)
	if err != nil {
		logger.Error("Failed to delete autoscaler Service", zap.Error(err))
	}
	logger.Info("Deleted autoscaler Service")

	err = c.deleteService(ctx, rev, ns)
	if err != nil {
		logger.Error("Failed to delete k8s service", zap.Error(err))
	}
	logger.Info("Deleted service")

	// And the deployment is no longer ready, so update that
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionFalse,
			Reason: "Inactive",
		})
	logger.Infof("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		logger.Error("Error recording inactivation of revision", zap.Error(err))
		return err
	}

	return nil
}

func (c *Receiver) createK8SResources(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	logger := logging.FromContext(ctx)
	// Fire off a Deployment..
	if err := c.reconcileDeployment(ctx, rev, ns); err != nil {
		logger.Error("Failed to create a deployment", zap.Error(err))
		return err
	}

	// Autoscale the service
	if err := c.reconcileAutoscalerDeployment(ctx, rev); err != nil {
		logger.Error("Failed to create autoscaler Deployment", zap.Error(err))
	}
	if err := c.reconcileAutoscalerService(ctx, rev); err != nil {
		logger.Error("Failed to create autoscaler Service", zap.Error(err))
	}
	if c.controllerConfig.EnableVarLogCollection {
		if err := c.reconcileFluentdConfigMap(ctx, rev); err != nil {
			logger.Error("Failed to create fluent config map", zap.Error(err))
		}
	}

	// Create k8s service
	serviceName, err := c.reconcileService(ctx, rev, ns)
	if err != nil {
		logger.Error("Failed to create k8s service", zap.Error(err))
	} else {
		rev.Status.ServiceName = serviceName
	}

	// Check to see if the revision has already been marked as ready and
	// don't mark it if it's already ready.
	// TODO: could always fetch the endpoint again and double-check it is still
	// ready.
	if rev.Status.IsReady() {
		return nil
	}

	// Checking existing revision condition to see if it is the initial deployment or
	// during the reactivating process. If a revision is in condition "Inactive" or "Activating",
	// we need to route traffic to the activator; if a revision is in condition "Deploying",
	// we need to route traffic to the revision directly.
	reason := "Deploying"
	cond := rev.Status.GetCondition(v1alpha1.RevisionConditionReady)
	if cond != nil {
		if (cond.Reason == "Inactive" && cond.Status == corev1.ConditionFalse) ||
			(cond.Reason == "Activating" && cond.Status == corev1.ConditionUnknown) {
			reason = "Activating"
		}
	}

	// By updating our deployment status we will trigger a Reconcile()
	// that will watch for service to become ready for serving traffic.
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionUnknown,
			Reason: reason,
		})
	logger.Infof("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		logger.Error("Error recording build completion", zap.Error(err))
		return err
	}

	return nil
}

func (c *Receiver) deleteDeployment(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	logger := logging.FromContext(ctx)
	deploymentName := controller.GetRevisionDeploymentName(rev)
	dc := c.KubeClientSet.AppsV1().Deployments(ns)
	if _, err := dc.Get(deploymentName, metav1.GetOptions{}); err != nil && apierrs.IsNotFound(err) {
		return nil
	}

	logger.Infof("Deleting Deployment %q", deploymentName)
	tmp := metav1.DeletePropagationForeground
	err := dc.Delete(deploymentName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("deployments.Delete for %q failed: %s", deploymentName, err)
		return err
	}
	return nil
}

func (c *Receiver) reconcileDeployment(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	logger := logging.FromContext(ctx)
	dc := c.KubeClientSet.AppsV1().Deployments(ns)
	// First, check if deployment exists already.
	deploymentName := controller.GetRevisionDeploymentName(rev)

	if _, err := dc.Get(deploymentName, metav1.GetOptions{}); err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("deployments.Get for %q failed: %s", deploymentName, err)
			return err
		}
		logger.Infof("Deployment %q doesn't exist, creating", deploymentName)
	} else {
		// TODO(mattmoor): Compare the deployments and update if it has changed
		// out from under us.
		logger.Infof("Found existing deployment %q", deploymentName)
		return nil
	}

	// Create the deployment.
	controllerRef := controller.NewRevisionControllerRef(rev)
	// Create a single pod so that it gets created before deployment->RS to try to speed
	// things up
	podSpec := MakeElaPodSpec(rev, c.controllerConfig)
	deployment := MakeElaDeployment(rev, ns)
	deployment.OwnerReferences = append(deployment.OwnerReferences, *controllerRef)

	deployment.Spec.Template.Spec = *podSpec

	// Resolve tag image references to digests.
	if err := c.resolver.Resolve(deployment); err != nil {
		logger.Error("Error resolving deployment", zap.Error(err))
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionContainerHealthy,
				Status:  corev1.ConditionFalse,
				Reason:  "ContainerMissing",
				Message: err.Error(),
			})
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  "ContainerMissing",
				Message: err.Error(),
			})
		if _, err := c.updateStatus(rev); err != nil {
			logger.Error("Error recording resolution problem", zap.Error(err))
			return err
		}
		return err
	}

	// Set the ProgressDeadlineSeconds
	deployment.Spec.ProgressDeadlineSeconds = new(int32)
	*deployment.Spec.ProgressDeadlineSeconds = progressDeadlineSeconds

	logger.Infof("Creating Deployment: %q", deployment.Name)
	_, createErr := dc.Create(deployment)

	return createErr
}

func (c *Receiver) deleteService(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	logger := logging.FromContext(ctx)
	sc := c.KubeClientSet.Core().Services(ns)
	serviceName := controller.GetElaK8SServiceNameForRevision(rev)

	logger.Infof("Deleting service %q", serviceName)
	tmp := metav1.DeletePropagationForeground
	err := sc.Delete(serviceName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("service.Delete for %q failed: %s", serviceName, err)
		return err
	}
	return nil
}

func (c *Receiver) reconcileService(ctx context.Context, rev *v1alpha1.Revision, ns string) (string, error) {
	logger := logging.FromContext(ctx)
	sc := c.KubeClientSet.Core().Services(ns)
	serviceName := controller.GetElaK8SServiceNameForRevision(rev)

	if _, err := sc.Get(serviceName, metav1.GetOptions{}); err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("services.Get for %q failed: %s", serviceName, err)
			return "", err
		}
		logger.Infof("serviceName %q doesn't exist, creating", serviceName)
	} else {
		// TODO(vaikas): Check that the service is legit and matches what we expect
		// to have there.
		logger.Infof("Found existing service %q", serviceName)
		return serviceName, nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	service := MakeRevisionK8sService(rev, ns)
	service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	logger.Infof("Creating service: %q", service.Name)
	_, err := sc.Create(service)
	return serviceName, err
}

func (c *Receiver) reconcileFluentdConfigMap(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	ns := rev.Namespace

	// One ConfigMap for Fluentd sidecar per namespace. It has multiple owner
	// references. Can not set blockOwnerDeletion and Controller to true.
	revRef := newRevisionNonControllerRef(rev)

	cmc := c.KubeClientSet.Core().ConfigMaps(ns)
	configMap, err := cmc.Get(fluentdConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("configmaps.Get for %q failed: %s", fluentdConfigMapName, err)
			return err
		}
		// ConfigMap doesn't exist, going to create it
		configMap = MakeFluentdConfigMap(ns, c.controllerConfig.FluentdSidecarOutputConfig)
		configMap.OwnerReferences = append(configMap.OwnerReferences, *revRef)
		logger.Infof("Creating configmap: %q", configMap.Name)
		_, err = cmc.Create(configMap)
		return err
	}

	// ConfigMap exists, going to update it
	desiredConfigMap := configMap.DeepCopy()
	desiredConfigMap.Data = map[string]string{
		"varlog.conf": makeFullFluentdConfig(c.controllerConfig.FluentdSidecarOutputConfig),
	}
	addOwnerReference(desiredConfigMap, revRef)
	if !reflect.DeepEqual(desiredConfigMap, configMap) {
		logger.Infof("Updating configmap: %q", desiredConfigMap.Name)
		_, err = cmc.Update(desiredConfigMap)
		return err
	}
	return nil
}

func (c *Receiver) deleteAutoscalerService(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	sc := c.KubeClientSet.Core().Services(AutoscalerNamespace)
	if _, err := sc.Get(autoscalerName, metav1.GetOptions{}); err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	logger.Infof("Deleting autoscaler Service %q", autoscalerName)
	tmp := metav1.DeletePropagationForeground
	err := sc.Delete(autoscalerName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("Autoscaler Service delete for %q failed: %s", autoscalerName, err)
		return err
	}
	return nil
}

func (c *Receiver) reconcileAutoscalerService(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	sc := c.KubeClientSet.Core().Services(AutoscalerNamespace)
	_, err := sc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("Autoscaler Service get for %q failed: %s", autoscalerName, err)
			return err
		}
		logger.Infof("Autoscaler Service %q doesn't exist, creating", autoscalerName)
	} else {
		logger.Infof("Found existing autoscaler Service %q", autoscalerName)
		return nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	service := MakeElaAutoscalerService(rev)
	service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	logger.Infof("Creating autoscaler Service: %q", service.Name)
	_, err = sc.Create(service)
	return err
}

func (c *Receiver) deleteAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	dc := c.KubeClientSet.AppsV1().Deployments(AutoscalerNamespace)
	_, err := dc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	logger.Infof("Deleting autoscaler Deployment %q", autoscalerName)
	tmp := metav1.DeletePropagationForeground
	err = dc.Delete(autoscalerName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("Autoscaler Deployment delete for %q failed: %s", autoscalerName, err)
		return err
	}
	return nil
}

func (c *Receiver) reconcileAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	dc := c.KubeClientSet.AppsV1().Deployments(AutoscalerNamespace)
	_, err := dc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("Autoscaler Deployment get for %q failed: %s", autoscalerName, err)
			return err
		}
		logger.Infof("Autoscaler Deployment %q doesn't exist, creating", autoscalerName)
	} else {
		logger.Infof("Found existing autoscaler Deployment %q", autoscalerName)
		return nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	deployment := MakeElaAutoscalerDeployment(rev, c.controllerConfig.AutoscalerImage)
	deployment.OwnerReferences = append(deployment.OwnerReferences, *controllerRef)
	logger.Infof("Creating autoscaler Deployment: %q", deployment.Name)
	_, err = dc.Create(deployment)
	return err
}
