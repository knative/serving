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

package autoscaling

import (
	"strings"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/scale"

	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
)

// kpaScaler scales the target of a KPA up or down including scaling to zero.
type kpaScaler struct {
	servingClientSet clientset.Interface
	scaleClientSet   scale.ScalesGetter
	logger           *zap.SugaredLogger
}

// NewKPAScaler creates a kpaScaler.
func NewKPAScaler(servingClientSet clientset.Interface, scaleClientSet scale.ScalesGetter, logger *zap.SugaredLogger) KPAScaler {
	return &kpaScaler{
		servingClientSet: servingClientSet,
		scaleClientSet:   scaleClientSet,
		logger:           logger,
	}
}

// Scale attempts to scale the given KPA's target reference to the desired scale.
func (rs *kpaScaler) Scale(kpa *kpa.PodAutoscaler, desiredScale int32) error {
	logger := loggerWithKPAInfo(rs.logger, kpa.Namespace, kpa.Name)

	if desiredScale < 0 {
		logger.Debug("Metrics are not yet being collected.")
		return nil
	}

	// TODO(mattmoor): Drop this once the KPA is the source of truth and we
	// scale exclusively on metrics.
	revGVK := v1alpha1.SchemeGroupVersion.WithKind("Revision")
	owner := metav1.GetControllerOf(kpa)
	if owner == nil || owner.Kind != revGVK.Kind ||
		owner.APIVersion != revGVK.GroupVersion().String() {
		logger.Debug("KPA is not owned by a Revision.")
		return nil
	}

	// Do not scale an inactive revision.
	revisionClient := rs.servingClientSet.ServingV1alpha1().Revisions(kpa.Namespace)
	rev, err := revisionClient.Get(owner.Name, metav1.GetOptions{})
	if err != nil {
		logger.Error("Unable to fetch Revision.", zap.Error(err))
		return err
	}

	gv, err := schema.ParseGroupVersion(kpa.Spec.ScaleTargetRef.APIVersion)
	if err != nil {
		logger.Error("Unable to parse APIVersion.", zap.Error(err))
		return err
	}
	resource := schema.GroupResource{
		Group: gv.Group,
		// TODO(mattmoor): Do something better than this.
		Resource: strings.ToLower(kpa.Spec.ScaleTargetRef.Kind) + "s",
	}
	resourceName := kpa.Spec.ScaleTargetRef.Name

	// Identify the current scale.
	scl, err := rs.scaleClientSet.Scales(kpa.Namespace).Get(resource, resourceName)
	if err != nil {
		logger.Errorf("Resource %q not found.", resourceName, zap.Error(err))
		return err
	}

	// When scaling to zero, flip the revision's ServingState to Reserve (if Active).
	if kpa.Spec.ServingState == v1alpha1.RevisionServingStateActive && desiredScale == 0 {
		// TODO(mattmoor): Delay the scale to zero until the LTT of "Active=False"
		// is some time in the past.
		logger.Debug("Setting revision ServingState to Reserve.")
		rev.Spec.ServingState = v1alpha1.RevisionServingStateReserve
		if _, err := revisionClient.Update(rev); err != nil {
			logger.Error("Error updating revision serving state.", zap.Error(err))
			return err
		}
		return nil
	}

	// When ServingState=Reserve (see above) propagates to us, then actually scale
	// things to zero.
	if kpa.Spec.ServingState != v1alpha1.RevisionServingStateActive {
		desiredScale = 0
	}

	currentScale := scl.Spec.Replicas
	if desiredScale == currentScale {
		return nil
	}
	logger.Infof("Scaling from %d to %d", currentScale, desiredScale)

	// Scale the target reference.
	scl.Spec.Replicas = desiredScale
	_, err = rs.scaleClientSet.Scales(kpa.Namespace).Update(resource, scl)
	if err != nil {
		logger.Errorf("Error scaling target reference %v.", resourceName, zap.Error(err))
		return err
	}

	logger.Debug("Successfully scaled.")
	return nil
}
