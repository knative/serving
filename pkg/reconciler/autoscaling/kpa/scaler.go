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

	"go.uber.org/zap"

	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/injection/clients/dynamicclient"
	"knative.dev/pkg/logging"

	pkgnet "knative.dev/pkg/network"
	"knative.dev/serving/pkg/activator"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	nv1a1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/prober"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	aresources "knative.dev/serving/pkg/reconciler/autoscaling/resources"
	rresources "knative.dev/serving/pkg/reconciler/revision/resources"
	"knative.dev/serving/pkg/resources"

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

	// TODO(#3456): Remove this buffer once KPA does pod failure diagnostics.
	//
	// KPA will scale the Deployment down to zero if it fails to activate after ProgressDeadlineSeconds,
	// however, after ProgressDeadlineSeconds, the Deployment itself updates its status, which causes
	// the Revision to re-reconcile and diagnose pod failures. If we use the same timeout here, we will
	// race the Revision reconciler and scale down the pods before it can actually surface the pod errors.
	// We should instead do pod failure diagnostics here immediately before scaling down the Deployment.
	activationTimeoutBuffer = 10 * time.Second
	activationTimeout       = time.Duration(rresources.ProgressDeadlineSeconds)*time.Second + activationTimeoutBuffer
)

var probeOptions = []interface{}{
	prober.WithHeader(network.ProbeHeaderName, activator.Name),
	prober.ExpectsBody(activator.Name),
	prober.ExpectsStatusCodes([]int{http.StatusOK}),
}

// for mocking in tests
type asyncProber interface {
	Offer(context.Context, string, interface{}, time.Duration, time.Duration, ...interface{}) bool
}

// scaler scales the target of a kpa-class PA up or down including scaling to zero.
type scaler struct {
	psInformerFactory duck.InformerFactory
	dynamicClient     dynamic.Interface
	transport         http.RoundTripper

	// For sync probes.
	activatorProbe func(pa *pav1alpha1.PodAutoscaler, transport http.RoundTripper) (bool, error)

	// For async probes.
	probeManager asyncProber
	enqueueCB    func(interface{}, time.Duration)
}

// newScaler creates a scaler.
func newScaler(ctx context.Context, psInformerFactory duck.InformerFactory, enqueueCB func(interface{}, time.Duration)) *scaler {
	logger := logging.FromContext(ctx)
	transport := network.NewProberTransport()
	ks := &scaler{
		// Wrap it in a cache, so that we don't stamp out a new
		// informer/lister each time.
		psInformerFactory: psInformerFactory,
		dynamicClient:     dynamicclient.Get(ctx),
		transport:         transport,

		// Production setup uses the default probe implementation.
		activatorProbe: activatorProbe,
		probeManager: prober.New(func(arg interface{}, success bool, err error) {
			logger.Infof("Async prober is done for %v: success?: %v error: %v", arg, success, err)
			// Re-enqeue the PA in any case. If the probe timed out to retry again, if succeeded to scale to 0.
			enqueueCB(arg, reenqeuePeriod)
		}, transport),
		enqueueCB: enqueueCB,
	}
	return ks
}

// Resolves the pa to hostname:port.
func paToProbeTarget(pa *pav1alpha1.PodAutoscaler) string {
	svc := pkgnet.GetServiceHostname(pa.Status.ServiceName, pa.Namespace)
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
	return prober.Do(context.Background(), transport, paToProbeTarget(pa), probeOptions...)
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

func (ks *scaler) handleScaleToZero(ctx context.Context, pa *pav1alpha1.PodAutoscaler,
	sks *nv1a1.ServerlessService, desiredScale int32) (int32, bool) {
	if desiredScale != 0 {
		return desiredScale, true
	}

	// We should only scale to zero when three of the following conditions are true:
	//   a) enable-scale-to-zero from configmap is true
	//   b) The PA has been active for at least the stable window, after which it gets marked inactive
	//   c) The PA has been inactive for at least the grace period
	config := config.FromContext(ctx).Autoscaler
	if !config.EnableScaleToZero {
		return 1, true
	}

	now := time.Now()
	logger := logging.FromContext(ctx)
	if pa.Status.IsActivating() { // Active=Unknown
		// If we are stuck activating for longer than our progress deadline, presume we cannot succeed and scale to 0.
		if pa.Status.CanFailActivation(now, activationTimeout) {
			logger.Infof("Activation has timed out after %v.", activationTimeout)
			return 0, true
		}
		ks.enqueueCB(pa, activationTimeout)
		return scaleUnknown, false
	} else if pa.Status.IsReady() { // Active=True
		// Don't scale-to-zero if the PA is active

		// Do not scale to 0, but return desiredScale of 0 to mark PA inactive.
		sw := aresources.StableWindow(pa, config)
		af := pa.Status.ActiveFor(now)
		if af >= sw {
			// We do not need to enqueue PA here, since this will
			// make SKS reconcile and when it's done, PA will be reconciled again.
			return desiredScale, false
		}
		// Otherwise, scale down to at most 1 for the remainder of the idle period and then
		// reconcile PA again.
		logger.Infof("Sleeping additionally for %v before can scale to 0", sw-af)
		ks.enqueueCB(pa, sw-af)
		desiredScale = 1
	} else { // Active=False
		r, err := ks.activatorProbe(pa, ks.transport)
		logger.Infof("Probing activator = %v, err = %v", r, err)
		if r {
			// This enforces that the revision has been backed by the activator for at least
			// ScaleToZeroGracePeriod time.
			// Note: SKS will always be present when scaling to zero, so nil checks are just
			// defensive programming.

			// Most conservative check, if it passes we're good.
			if pa.Status.CanScaleToZero(now, config.ScaleToZeroGracePeriod) {
				return desiredScale, true
			}

			// Otherwise check how long SKS was in proxy mode.
			to := config.ScaleToZeroGracePeriod
			if sks != nil {
				// Compute the difference between time we've been proxying with the timeout.
				// If it's positive, that's the time we need to sleep, if negative -- we
				// can scale to zero.
				to -= sks.Status.ProxyFor()
				if to <= 0 {
					logger.Infof("Fast path scaling to 0, in proxy mode for: %v", sks.Status.ProxyFor())
					return desiredScale, true
				}
			}

			// Re-enqeue the PA for reconciliation with timeout of `to` to make sure we wait
			// long enough.
			ks.enqueueCB(pa, to)
			return desiredScale, false
		}

		// Otherwise (any prober failure) start the async probe.
		logger.Info("PA is not yet backed by activator, cannot scale to zero")
		if !ks.probeManager.Offer(context.Background(), paToProbeTarget(pa), pa, probePeriod, probeTimeout, probeOptions...) {
			logger.Info("Probe for revision is already in flight")
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
		patchBytes, metav1.PatchOptions{})
	if err != nil {
		logger.Errorw("Error scaling target reference "+name, zap.Error(err))
		return desiredScale, err
	}

	logger.Debug("Successfully scaled.")
	return desiredScale, nil
}

// Scale attempts to scale the given PA's target reference to the desired scale.
func (ks *scaler) Scale(ctx context.Context, pa *pav1alpha1.PodAutoscaler, sks *nv1a1.ServerlessService, desiredScale int32) (int32, error) {
	logger := logging.FromContext(ctx)

	if desiredScale < 0 && !pa.Status.IsActivating() {
		logger.Debug("Metrics are not yet being collected.")
		return desiredScale, nil
	}

	min, max := pa.ScaleBounds()
	if newScale := applyBounds(min, max, desiredScale); newScale != desiredScale {
		logger.Debugf("Adjusting desiredScale to meet the min and max bounds before applying: %d -> %d", desiredScale, newScale)
		desiredScale = newScale
	}

	desiredScale, shouldApplyScale := ks.handleScaleToZero(ctx, pa, sks, desiredScale)
	if !shouldApplyScale {
		return desiredScale, nil
	}

	ps, err := resources.GetScaleResource(pa.Namespace, pa.Spec.ScaleTargetRef, ks.psInformerFactory)
	if err != nil {
		logger.Errorw(fmt.Sprintf("Resource %v not found", pa.Spec.ScaleTargetRef), zap.Error(err))
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
