/*
Copyright 2019 The Knative Authors

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
	"net/http"
	"time"

	"github.com/knative/pkg/apis/duck"
	"github.com/knative/pkg/injection/clients/dynamicclient"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/activator"
	pav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/network/prober"
	"github.com/knative/serving/pkg/reconciler/autoscaling/config"
	"github.com/knative/serving/pkg/resources"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	scaleUnknown = -1
	probePeriod  = 1 * time.Second
	probeTimeout = 45 * time.Second
	// The time after which the PA will be re-enqueued.
	// This number is small, since `handleScaleToZero` below will
	// re-enque for the configured grace period.
	reenqeuePeriod = 1 * time.Second
)

// for mocking in tests
type asyncProber interface {
	Offer(context.Context, string, string, interface{}, time.Duration, time.Duration) bool
}

// scaler scales the target of a kpa-class PA up or down including scaling to zero.
type scaler struct {
	psInformerFactory duck.InformerFactory
	dynamicClient     dynamic.Interface
	logger            *zap.SugaredLogger
	transportFactory  prober.TransportFactory

	// For sync probes.
	activatorProbe func(pa *pav1alpha1.PodAutoscaler, transport http.RoundTripper) (bool, error)

	// For async probes.
	probeManager asyncProber
	enqueueCB    func(interface{}, time.Duration)
}

// newScaler creates a scaler.
func newScaler(ctx context.Context, psInformerFactory duck.InformerFactory, enqueueCB func(interface{}, time.Duration)) *scaler {
	logger := logging.FromContext(ctx)
	ks := &scaler{
		// Wrap it in a cache, so that we don't stamp out a new
		// informer/lister each time.
		psInformerFactory: psInformerFactory,
		dynamicClient:     dynamicclient.Get(ctx),
		logger:            logger,
		transportFactory: func() http.RoundTripper {
			return network.NewAutoTransport()
		},

		// Production setup uses the default probe implementation.
		activatorProbe: activatorProbe,
		probeManager: prober.New(func(arg interface{}, success bool, err error) {
			logger.Infof("Async prober is done for %v: success?: %v error: %v", arg, success, err)
			// Re-enqeue the PA in any case. If the probe timed out to retry again, if succeeded to scale to 0.
			enqueueCB(arg, reenqeuePeriod)
		}, network.NewAutoTransport),
		enqueueCB: enqueueCB,
	}
	return ks
}

// Resolves the pa to hostname:port.
func paToProbeTarget(pa *pav1alpha1.PodAutoscaler) string {
	svc := network.GetServiceHostname(pa.Status.ServiceName, pa.Namespace)
	port := networking.ServicePort(pa.Spec.ProtocolType)
	return fmt.Sprintf("http://%s:%d/", svc, port)
}

// activatorProbe returns true if via probe it determines that the
// PA is backed by the Activator.
func activatorProbe(pa *pav1alpha1.PodAutoscaler, transport http.RoundTripper) (bool, error) {
	// No service name -- no probe.
	if pa.Status.ServiceName == "" {
		return false, nil
	}
	return prober.Do(context.Background(), transport, paToProbeTarget(pa), activator.Name)
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

func (ks *scaler) handleScaleToZero(pa *pav1alpha1.PodAutoscaler, desiredScale int32, config *autoscaler.Config) (int32, bool) {
	if desiredScale != 0 {
		return desiredScale, true
	}

	// We should only scale to zero when three of the following conditions are true:
	//   a) enable-scale-to-zero from configmap is true
	//   b) The PA has been active for at least the stable window, after which it gets marked inactive
	//   c) The PA has been inactive for at least the grace period

	if !config.EnableScaleToZero {
		return 1, true
	}

	if pa.Status.IsActivating() { // Active=Unknown
		return scaleUnknown, false
	} else if pa.Status.IsReady() { // Active=True
		// Don't scale-to-zero if the PA is active

		// Do not scale to 0, but return desiredScale of 0 to mark PA inactive.
		if pa.Status.CanMarkInactive(config.StableWindow) {
			// We do not need to enqueue PA here, since this will
			// make SKS reconcile and when it's done, PA will be reconciled again.
			return desiredScale, false
		}
		// Otherwise, scale down to 1 until the idle period elapses and re-enqueue
		// the PA for reconciliation at that time.
		ks.enqueueCB(pa, config.StableWindow)
		desiredScale = 1
	} else { // Active=False
		r, err := ks.activatorProbe(pa, ks.transportFactory())
		ks.logger.Infof("%s probing activator = %v, err = %v", pa.Name, r, err)
		if r {
			// Make sure we've been inactive for enough time.
			if pa.Status.CanScaleToZero(config.ScaleToZeroGracePeriod) {
				return desiredScale, true
			}
			// Re-enqeue the PA for reconciliation after grace period.
			// In istio-lean this can be close to 0.
			ks.enqueueCB(pa, config.ScaleToZeroGracePeriod)
			return desiredScale, false
		}

		// Otherwise (any prober failure) start the async probe.
		ks.logger.Infof("%s is not yet backed by activator, cannot scale to zero", pa.Name)
		if !ks.probeManager.Offer(context.Background(), paToProbeTarget(pa), activator.Name, pa, probePeriod, probeTimeout) {
			ks.logger.Infof("Probe for %s is already in flight", pa.Name)
		}
		return desiredScale, false
	}

	return desiredScale, true
}

func (ks *scaler) applyScale(ctx context.Context, pa *pav1alpha1.PodAutoscaler, desiredScale int32,
	ps *pav1alpha1.PodScalable) (int32, error) {
	logger := logging.FromContext(ctx)

	gvr, name, err := resources.ScaleResourceArguments(pa.Spec.ScaleTargetRef)
	if err != nil {
		return desiredScale, err
	}

	psNew := ps.DeepCopy()
	psNew.Spec.Replicas = &desiredScale
	patch, err := duck.CreatePatch(ps, psNew)
	if err != nil {
		return desiredScale, err
	}
	patchBytes, err := patch.MarshalJSON()
	if err != nil {
		return desiredScale, err
	}

	_, err = ks.dynamicClient.Resource(*gvr).Namespace(pa.Namespace).Patch(ps.Name, types.JSONPatchType,
		patchBytes, metav1.UpdateOptions{})
	if err != nil {
		logger.Errorw(fmt.Sprintf("Error scaling target reference %s", name), zap.Error(err))
		return desiredScale, err
	}

	logger.Debug("Successfully scaled.")
	return desiredScale, nil
}

// Scale attempts to scale the given PA's target reference to the desired scale.
func (ks *scaler) Scale(ctx context.Context, pa *pav1alpha1.PodAutoscaler, desiredScale int32) (int32, error) {
	logger := logging.FromContext(ctx)

	if desiredScale < 0 {
		logger.Debug("Metrics are not yet being collected.")
		return desiredScale, nil
	}

	min, max := pa.ScaleBounds()
	if newScale := applyBounds(min, max, desiredScale); newScale != desiredScale {
		logger.Debugf("Adjusting desiredScale to meet the min and max bounds before applying: %d -> %d", desiredScale, newScale)
		desiredScale = newScale
	}

	desiredScale, shouldApplyScale := ks.handleScaleToZero(pa, desiredScale, config.FromContext(ctx).Autoscaler)
	if !shouldApplyScale {
		return desiredScale, nil
	}

	ps, err := resources.GetScaleResource(pa.Namespace, pa.Spec.ScaleTargetRef, ks.psInformerFactory)
	if err != nil {
		logger.Errorw(fmt.Sprintf("Resource %q not found", pa.Name), zap.Error(err))
		return desiredScale, err
	}

	currentScale := int32(1)
	if ps.Spec.Replicas != nil {
		currentScale = *ps.Spec.Replicas
	}
	if desiredScale == currentScale {
		return desiredScale, nil
	}

	logger.Infof("Scaling from %d to %d", currentScale, desiredScale)
	return ks.applyScale(ctx, pa, desiredScale, ps)
}
