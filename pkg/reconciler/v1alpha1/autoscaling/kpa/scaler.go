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

package kpa

import (
	"context"
	"fmt"
	"sync"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/logging"
	pav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	perrors "github.com/pkg/errors"
	"go.uber.org/zap"
	autoscalingapi "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/scale"
)

const scaleUnknown = -1

// scaler scales the target of a kpa-class PA up or down including scaling to zero.
type scaler struct {
	servingClientSet clientset.Interface
	scaleClientSet   scale.ScalesGetter
	logger           *zap.SugaredLogger

	// autoscalerConfig could change over time and access to it
	// must go through autoscalerConfigMutex
	autoscalerConfig      *autoscaler.Config
	autoscalerConfigMutex sync.Mutex
}

// NewScaler creates a scaler.
func NewScaler(servingClientSet clientset.Interface, scaleClientSet scale.ScalesGetter,
	logger *zap.SugaredLogger, configMapWatcher configmap.Watcher) Scaler {
	ks := &scaler{
		servingClientSet: servingClientSet,
		scaleClientSet:   scaleClientSet,
		logger:           logger,
	}

	// Watch for config changes.
	configMapWatcher.Watch(autoscaler.ConfigName, ks.receiveAutoscalerConfig)
	return ks
}

func (ks *scaler) receiveAutoscalerConfig(configMap *corev1.ConfigMap) {
	newAutoscalerConfig, err := autoscaler.NewConfigFromConfigMap(configMap)
	ks.autoscalerConfigMutex.Lock()
	defer ks.autoscalerConfigMutex.Unlock()
	if err != nil {
		if ks.autoscalerConfig != nil {
			ks.logger.Errorf("Error updating Autoscaler ConfigMap: %v", err)
		} else {
			ks.logger.Fatalf("Error initializing Autoscaler ConfigMap: %v", err)
		}
		return
	}
	ks.logger.Infof("Autoscaler config map is added or updated: %v", configMap)
	ks.autoscalerConfig = newAutoscalerConfig
}

func (ks *scaler) getAutoscalerConfig() *autoscaler.Config {
	ks.autoscalerConfigMutex.Lock()
	defer ks.autoscalerConfigMutex.Unlock()
	return ks.autoscalerConfig.DeepCopy()
}

// pre: 0 <= min <= max && 0 <= x
func applyBounds(min, max, x int32) int32 {
	if x < min {
		return min
	}
	if max != 0 && x > max {
		return max
	}
	return x
}

// GetScaleResource returns the current scale resource for the PA.
func (ks *scaler) GetScaleResource(pa *pav1alpha1.PodAutoscaler) (*autoscalingapi.Scale, error) {
	resource, resourceName, err := scaleResourceArgs(pa)
	if err != nil {
		return nil, perrors.Wrap(err, "Error Get'ting /scale resource")
	}

	// Identify the current scale.
	return ks.scaleClientSet.Scales(pa.Namespace).Get(*resource, resourceName)
}

// scaleResourceArgs returns GroupResource and the resource name, from the PA resource.
func scaleResourceArgs(pa *pav1alpha1.PodAutoscaler) (*schema.GroupResource, string, error) {
	gv, err := schema.ParseGroupVersion(pa.Spec.ScaleTargetRef.APIVersion)
	if err != nil {
		return nil, "", err
	}
	resource := apis.KindToResource(gv.WithKind(pa.Spec.ScaleTargetRef.Kind)).GroupResource()
	return &resource, pa.Spec.ScaleTargetRef.Name, nil
}

func isPAOwnedByRevision(ctx context.Context, pa *pav1alpha1.PodAutoscaler) bool {
	logger := logging.FromContext(ctx)

	// TODO(mattmoor): Drop this once the KPA is the source of truth and we
	// scale exclusively on metrics.
	revGVK := v1alpha1.SchemeGroupVersion.WithKind("Revision")
	owner := metav1.GetControllerOf(pa)
	if owner == nil || owner.Kind != revGVK.Kind ||
		owner.APIVersion != revGVK.GroupVersion().String() {
		logger.Debug("PA is not owned by a Revision.")
		return true
	}
	return false
}

func (ks *scaler) handleScaleToZero(pa *pav1alpha1.PodAutoscaler, desiredScale int32) (int32, bool) {
	if desiredScale == 0 {
		// We should only scale to zero when both of the following conditions are true:
		//   a) The PA has been active for atleast the stable window, after which it gets marked inactive
		//   b) The PA has been inactive for atleast the grace period

		config := ks.getAutoscalerConfig()

		if pa.Status.IsActivating() { // Active=Unknown
			// Don't scale-to-zero during activation
			desiredScale = scaleUnknown
		} else if pa.Status.IsReady() { // Active=True
			// Don't scale-to-zero if the PA is active

			// Only let a revision be scaled to 0 if it's been active for at
			// least the stable window's time.
			if pa.Status.CanMarkInactive(config.StableWindow) {
				return desiredScale, true
			}
			// Otherwise, scale down to 1 until the idle period elapses
			desiredScale = 1
		} else { // Active=False
			// Don't scale-to-zero if the grace period hasn't elapsed
			if !pa.Status.CanScaleToZero(config.ScaleToZeroGracePeriod) {
				return desiredScale, true
			}
		}
	}
	return desiredScale, false
}

func (ks *scaler) applyScale(ctx context.Context, pa *pav1alpha1.PodAutoscaler, desiredScale int32, resource *schema.GroupResource, scl *autoscalingapi.Scale) (int32, error) {
	logger := logging.FromContext(ctx)

	// Scale the target reference.
	scl.Spec.Replicas = desiredScale
	_, err := ks.scaleClientSet.Scales(pa.Namespace).Update(*resource, scl)
	if err != nil {
		resourceName := pa.Spec.ScaleTargetRef.Name
		logger.Errorw(fmt.Sprintf("Error scaling target reference %s", resourceName), zap.Error(err))
		return desiredScale, err
	}

	logger.Debug("Successfully scaled.")
	return desiredScale, nil

}

// Scale attempts to scale the given PA's target reference to the desired scale.
func (ks *scaler) Scale(ctx context.Context, pa *pav1alpha1.PodAutoscaler, desiredScale int32) (int32, error) {
	logger := logging.FromContext(ctx)

	if isPAOwnedByRevision(ctx, pa) {
		return desiredScale, nil
	}

	resource, resourceName, err := scaleResourceArgs(pa)
	if err != nil {
		logger.Errorw("Unable to parse APIVersion", zap.Error(err))
		return desiredScale, err
	}

	// Identify the current scale.
	scl, err := ks.scaleClientSet.Scales(pa.Namespace).Get(*resource, resourceName)
	if err != nil {
		logger.Errorw(fmt.Sprintf("Resource %q not found", resourceName), zap.Error(err))
		return desiredScale, err
	}
	currentScale := scl.Spec.Replicas

	min, max := pa.ScaleBounds()
	if newScale := applyBounds(min, max, desiredScale); newScale != desiredScale {
		logger.Debugf("Adjusting desiredScale: %d -> %d", desiredScale, newScale)
		desiredScale = newScale
	}

	desiredScale, skipApplyScale := ks.handleScaleToZero(pa, desiredScale)
	if skipApplyScale {
		return desiredScale, nil
	}

	// Scale from zero. When there are no metrics scale to 1.
	if currentScale == 0 && desiredScale == scaleUnknown {
		logger.Debugf("Scaling up from 0 to 1")
		desiredScale = 1
	}

	if desiredScale < 0 {
		logger.Debug("Metrics are not yet being collected.")
		return desiredScale, nil
	}

	if desiredScale == currentScale {
		return desiredScale, nil
	}
	logger.Infof("Scaling from %d to %d", currentScale, desiredScale)

	return ks.applyScale(ctx, pa, desiredScale, resource, scl)
}
