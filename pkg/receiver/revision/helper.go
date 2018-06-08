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
	"strings"
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	// TODO(mattmoor): Move controller deps into receiver
	"github.com/knative/serving/pkg/controller"
)

// loggerWithRevisionInfo enriches the logs with revision name and namespace.
func loggerWithRevisionInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Revision, name))
}

func (c *Receiver) updateRevisionLoggingURL(rev *v1alpha1.Revision) error {
	logURLTmpl := c.controllerConfig.LoggingURLTemplate
	if logURLTmpl == "" {
		return nil
	}

	url := strings.Replace(logURLTmpl, "${REVISION_UID}", string(rev.UID), -1)

	if rev.Status.LogURL == url {
		return nil
	}
	rev.Status.LogURL = url
	_, err := c.updateStatus(rev)
	return err
}

// Checks whether the Revision knows whether the build is done.
// TODO(mattmoor): Use a method on the Build type.
func isBuildDone(rev *v1alpha1.Revision) (done, failed bool) {
	if rev.Spec.BuildName == "" {
		return true, false
	}
	for _, cond := range rev.Status.Conditions {
		if cond.Type != v1alpha1.RevisionConditionBuildSucceeded {
			continue
		}
		switch cond.Status {
		case corev1.ConditionTrue:
			return true, false
		case corev1.ConditionFalse:
			return true, true
		case corev1.ConditionUnknown:
			return false, false
		}
	}
	return false, false
}

func (c *Receiver) markRevisionReady(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	logger.Info("Marking Revision ready")
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
			Reason: "ServiceReady",
		})
	_, err := c.updateStatus(rev)
	return err
}

func (c *Receiver) markRevisionFailed(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	logger.Info("Marking Revision failed")
	reason, message := "ServiceTimeout", "Timed out waiting for a service endpoint to become ready"
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:    v1alpha1.RevisionConditionResourcesAvailable,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:    v1alpha1.RevisionConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	_, err := c.updateStatus(rev)
	return err
}

func (c *Receiver) markRevisionBuilding(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	reason := "Building"
	logger.Infof("Marking Revision %s", reason)
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionBuildSucceeded,
			Status: corev1.ConditionUnknown,
			Reason: reason,
		})
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionUnknown,
			Reason: reason,
		})
	// Let this trigger a reconciliation loop.
	_, err := c.updateStatus(rev)
	return err
}

func (c *Receiver) markBuildComplete(rev *v1alpha1.Revision, bc *buildv1alpha1.BuildCondition) error {
	switch bc.Type {
	case buildv1alpha1.BuildComplete:
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:   v1alpha1.RevisionConditionBuildSucceeded,
				Status: corev1.ConditionTrue,
			})
		c.Recorder.Event(rev, corev1.EventTypeNormal, "BuildComplete", bc.Message)
	case buildv1alpha1.BuildFailed, buildv1alpha1.BuildInvalid:
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionBuildSucceeded,
				Status:  corev1.ConditionFalse,
				Reason:  bc.Reason,
				Message: bc.Message,
			})
		rev.Status.SetCondition(
			&v1alpha1.RevisionCondition{
				Type:    v1alpha1.RevisionConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  bc.Reason,
				Message: bc.Message,
			})
		c.Recorder.Event(rev, corev1.EventTypeWarning, "BuildFailed", bc.Message)
	}
	// This will trigger a reconciliation that will cause us to stop tracking the build.
	_, err := c.updateStatus(rev)
	return err
}

func getBuildDoneCondition(build *buildv1alpha1.Build) *buildv1alpha1.BuildCondition {
	for _, cond := range build.Status.Conditions {
		if cond.Status != corev1.ConditionTrue {
			continue
		}
		switch cond.Type {
		case buildv1alpha1.BuildComplete, buildv1alpha1.BuildFailed, buildv1alpha1.BuildInvalid:
			return &cond
		}
	}
	return nil
}

func getIsServiceReady(e *corev1.Endpoints) bool {
	for _, es := range e.Subsets {
		if len(es.Addresses) > 0 {
			return true
		}
	}
	return false
}

func getRevisionLastTransitionTime(r *v1alpha1.Revision) time.Time {
	condCount := len(r.Status.Conditions)
	if condCount == 0 {
		return r.CreationTimestamp.Time
	}
	return r.Status.Conditions[condCount-1].LastTransitionTime.Time
}

func getDeploymentProgressCondition(deployment *appsv1.Deployment) *appsv1.DeploymentCondition {
	//as per https://kubernetes.io/docs/concepts/workloads/controllers/deployment
	for _, cond := range deployment.Status.Conditions {
		// Look for Deployment with status False
		if cond.Status != corev1.ConditionFalse {
			continue
		}
		// with Type Progressing and Reason Timeout
		// TODO (arvtiwar): hard coding "ProgressDeadlineExceeded" to avoid import kubernetes/kubernetes
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			return &cond
		}
	}
	return nil
}

func newRevisionNonControllerRef(rev *v1alpha1.Revision) *metav1.OwnerReference {
	blockOwnerDeletion := false
	isController := false
	revRef := controller.NewRevisionControllerRef(rev)
	revRef.BlockOwnerDeletion = &blockOwnerDeletion
	revRef.Controller = &isController
	return revRef
}

func addOwnerReference(configMap *corev1.ConfigMap, ownerReference *metav1.OwnerReference) {
	isOwner := false
	for _, existingOwner := range configMap.OwnerReferences {
		if ownerReference.Name == existingOwner.Name {
			isOwner = true
			break
		}
	}
	if !isOwner {
		configMap.OwnerReferences = append(configMap.OwnerReferences, *ownerReference)
	}
}

func (c *Receiver) removeFinalizers(ctx context.Context, rev *v1alpha1.Revision, ns string) error {
	logger := logging.FromContext(ctx)
	logger.Infof("Removing finalizers for %q\n", rev.Name)
	accessor, err := meta.Accessor(rev)
	if err != nil {
		logger.Panic("Failed to get metadata", zap.Error(err))
	}
	finalizers := accessor.GetFinalizers()
	for i, v := range finalizers {
		if v == "controller" {
			finalizers = append(finalizers[:i], finalizers[i+1:]...)
		}
	}
	accessor.SetFinalizers(finalizers)
	prClient := c.ElaClientSet.ServingV1alpha1().Revisions(rev.Namespace)
	prClient.Update(rev)
	logger.Infof("The finalizer 'controller' is removed.")

	return nil
}

func (c *Receiver) updateStatus(rev *v1alpha1.Revision) (*v1alpha1.Revision, error) {
	prClient := c.ElaClientSet.ServingV1alpha1().Revisions(rev.Namespace)
	newRev, err := prClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newRev.Status = rev.Status

	// TODO: for CRD there's no updatestatus, so use normal update
	return prClient.Update(newRev)
	//	return prClient.UpdateStatus(newRev)
}

// Given an endpoint see if it's managed by us and return the
// revision that created it.
// TODO: Consider using OwnerReferences.
// https://github.com/kubernetes/sample-controller/blob/master/controller.go#L373-L384
func lookupServiceOwner(endpoint *corev1.Endpoints) string {
	// see if there's a label on this object marking it as ours.
	if revisionName, ok := endpoint.Labels[serving.RevisionLabelKey]; ok {
		return revisionName
	}
	return ""
}
