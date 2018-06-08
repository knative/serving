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

package configuration

import (
	"context"
	"fmt"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// loggerWithConfigInfo enriches the logs with configuration name and namespace.
func loggerWithConfigInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Configuration, name))
}

func generateRevisionName(u *v1alpha1.Configuration) (string, error) {
	// TODO: consider making sure the length of the
	// string will not cause problems down the stack
	if u.Spec.Generation == 0 {
		return "", fmt.Errorf("configuration generation cannot be 0")
	}
	return fmt.Sprintf("%s-%05d", u.Name, u.Spec.Generation), nil
}

func (c *Receiver) updateStatus(u *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {
	configClient := c.ElaClientSet.ServingV1alpha1().Configurations(u.Namespace)
	newu, err := configClient.Get(u.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newu.Status = u.Status

	// TODO: for CRD there's no updatestatus, so use normal update
	return configClient.Update(newu)
	//	return configClient.UpdateStatus(newu)
}

func getLatestRevisionStatusCondition(revision *v1alpha1.Revision) *v1alpha1.RevisionCondition {
	for _, cond := range revision.Status.Conditions {
		if !(cond.Type == v1alpha1.RevisionConditionReady && cond.Status == corev1.ConditionTrue) {
			return &cond
		}
	}
	return nil
}

// Mark ConfigurationConditionReady of Configuration ready as the given latest
// created revision is ready.
func (c *Receiver) markConfigurationReady(
	ctx context.Context,
	config *v1alpha1.Configuration, revision *v1alpha1.Revision) {
	logger := logging.FromContext(ctx)
	logger.Info("Marking Configuration ready")
	config.Status.RemoveCondition(v1alpha1.ConfigurationConditionLatestRevisionReady)
	config.Status.SetCondition(
		&v1alpha1.ConfigurationCondition{
			Type:   v1alpha1.ConfigurationConditionReady,
			Status: corev1.ConditionTrue,
			Reason: "LatestRevisionReady",
		})
}

// Mark ConfigurationConditionLatestRevisionReady of Configuration to false with the status
// from the revision
func (c *Receiver) markConfigurationLatestRevisionStatus(
	ctx context.Context,
	config *v1alpha1.Configuration, revision *v1alpha1.Revision) {
	logger := logging.FromContext(ctx)
	config.Status.RemoveCondition(v1alpha1.ConfigurationConditionReady)
	cond := getLatestRevisionStatusCondition(revision)
	if cond == nil {
		logger.Info("Revision status is not updated yet")
		return
	}
	config.Status.SetCondition(
		&v1alpha1.ConfigurationCondition{
			Type:    v1alpha1.ConfigurationConditionLatestRevisionReady,
			Status:  corev1.ConditionFalse,
			Reason:  cond.Reason,
			Message: cond.Message,
		})
}
