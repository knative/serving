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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

// SyncDeployment implements deployment.Receiver
func (c *Receiver) SyncDeployment(deployment *appsv1.Deployment) error {
	cond := getDeploymentProgressCondition(deployment)
	if cond == nil {
		return nil
	}

	//Get the handle of Revision in context
	revName := deployment.Name
	namespace := deployment.Namespace
	logger := loggerWithRevisionInfo(c.Logger, namespace, revName)

	rev, err := c.lister.Revisions(namespace).Get(revName)
	if err != nil {
		return err
	}
	//Set the revision condition reason to ProgressDeadlineExceeded
	rev.Status.SetCondition(
		&v1alpha1.RevisionCondition{
			Type:    v1alpha1.RevisionConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  "ProgressDeadlineExceeded",
			Message: fmt.Sprintf("Unable to create pods for more than %d seconds.", progressDeadlineSeconds),
		})

	logger.Infof("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		return err
	}
	c.Recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded", "Revision %s not ready due to Deployment timeout", revName)
	return nil
}
