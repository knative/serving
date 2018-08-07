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

package autoscaler

import (
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/scale"
)

// revisionScaler scales revisions up or down including scaling to zero.
type revisionScaler struct {
	servingClientSet clientset.Interface
	scaleClientSet   scale.ScalesGetter
	logger           *zap.SugaredLogger
}

// NewRevisionScaler creates a revisionScaler.
func NewRevisionScaler(servingClientSet clientset.Interface, scaleClientSet scale.ScalesGetter, logger *zap.SugaredLogger) RevisionScaler {
	return &revisionScaler{
		servingClientSet: servingClientSet,
		scaleClientSet:   scaleClientSet,
		logger:           logger,
	}
}

// Scale attempts to scale the given revision to the desired scale.
func (rs *revisionScaler) Scale(oldRev *v1alpha1.Revision, desiredScale int32) {
	logger := loggerWithRevisionInfo(rs.logger, oldRev.Namespace, oldRev.Name)

	// Do not scale an inactive revision.
	// FIXME: given the input oldRev is stale, it might be better to pass in the revision's name and namespace instead.
	revisionClient := rs.servingClientSet.ServingV1alpha1().Revisions(oldRev.Namespace)
	rev, err := revisionClient.Get(oldRev.Name, metav1.GetOptions{})
	if err == nil && rev.Spec.ServingState != v1alpha1.RevisionServingStateActive {
		return
	}

	deploymentName := names.Deployment(oldRev)
	resource := schema.GroupResource{
		Group:    "apps",
		Resource: "deployments",
	}

	// Identify the current scale.
	scl, err := rs.scaleClientSet.Scales(oldRev.Namespace).Get(resource, deploymentName)
	if err != nil {
		logger.Error("Deployment not found.", zap.String("deployment", deploymentName), zap.Error(err))
		return
	}
	currentScale := scl.Spec.Replicas

	if desiredScale == currentScale {
		return
	}

	logger.Infof("Scaling from %d to %d", currentScale, desiredScale)

	// Don't scale if current scale is zero. Rely on the activator to scale
	// from zero.
	if currentScale == 0 {
		logger.Info("Cannot scale: Current scale is 0; activator must scale from 0.")
		return
	}

	// When scaling to zero, flip the revision's ServingState to Reserve.
	if desiredScale == 0 {
		logger.Debug("Setting revision ServingState to Reserve.")
		rev.Spec.ServingState = v1alpha1.RevisionServingStateReserve
		if _, err := revisionClient.Update(rev); err != nil {
			logger.Error("Error updating revision serving state.", zap.Error(err))
		}
		return
	}

	// Scale the deployment.
	scl.Spec.Replicas = desiredScale
	_, err = rs.scaleClientSet.Scales(oldRev.Namespace).Update(resource, scl)
	if err != nil {
		logger.Error("Error scaling deployment.", zap.String("deployment", deploymentName), zap.Error(err))
		return
	}

	logger.Debug("Successfully scaled.")
}
