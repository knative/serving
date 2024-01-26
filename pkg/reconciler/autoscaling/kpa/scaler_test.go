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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	// These are the fake informers we want setup.
	fakedynamicclient "knative.dev/pkg/injection/clients/dynamicclient/fake"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	podscalable "knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable/fake"

	nv1a1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	filteredinformerfactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
	"knative.dev/pkg/network"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	revisionresources "knative.dev/serving/pkg/reconciler/revision/resources"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
	key           = testNamespace + "/" + testRevision
)

func TestScaler(t *testing.T) {
	const activationTimeout = progressDeadline + activationTimeoutBuffer
	tests := []struct {
		label               string
		startReplicas       int
		scaleTo             int32
		minScale            int32
		maxScale            int32
		wantReplicas        int32
		wantScaling         bool
		sks                 SKSOption
		paMutation          func(*autoscalingv1alpha1.PodAutoscaler)
		proberfunc          func(*autoscalingv1alpha1.PodAutoscaler, http.RoundTripper) (bool, error)
		configMutator       func(*config.Config)
		wantCBCount         int
		wantAsyncProbeCount int
	}{{
		label:         "waits to scale to zero (just before idle period)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActive(k, time.Now().Add(-stableWindow).Add(1*time.Second))
		},
		wantCBCount: 1,
	}, {
		// Custom window will be shorter in the tests with custom PA window.
		label:         "waits to scale to zero (just before idle period), custom PA window",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			WithWindowAnnotation(paStableWindow.String())(k)
			paMarkActive(k, time.Now().Add(-paStableWindow).Add(1*time.Second))
		},
		wantCBCount: 1,
	}, {
		label:         "custom PA window, check for standard window, no probe",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			WithWindowAnnotation(paStableWindow.String())(k)
			paMarkActive(k, time.Now().Add(-stableWindow))
		},
	}, {
		label:         "scale to 1 waiting for idle expires",
		startReplicas: 10,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActive(k, time.Now().Add(-stableWindow).Add(1*time.Second))
		},
		wantCBCount: 1,
	}, {
		label:         "waits to scale to zero after idle period",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActive(k, time.Now().Add(-stableWindow))
		},
	}, {
		label:         "waits to scale to zero after idle period; sks in proxy mode",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActive(k, time.Now().Add(-stableWindow))
		},
		sks: func(s *nv1a1.ServerlessService) {
			s.Spec.Mode = nv1a1.SKSOperationModeProxy
		},
		wantCBCount: 1,
	}, {
		label:         "waits to scale to zero after idle period (custom PA window)",
		startReplicas: 1,
		scaleTo:       0,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			WithWindowAnnotation(paStableWindow.String())(k)
			paMarkActive(k, time.Now().Add(-paStableWindow))
		},
	}, {
		label:         "can scale to zero after grace period, but 0 PA retention",
		startReplicas: 1,
		scaleTo:       0,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
			k.Annotations[autoscaling.ScaleToZeroPodRetentionPeriodKey] = "0"
		},
		configMutator: func(c *config.Config) {
			c.Autoscaler.ScaleToZeroPodRetentionPeriod = 2 * gracePeriod
		},
		wantScaling: true,
	}, {
		label:         "can't scale to zero after grace period, but before last pod retention",
		startReplicas: 1,
		scaleTo:       0,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
		},
		configMutator: func(c *config.Config) {
			c.Autoscaler.ScaleToZeroPodRetentionPeriod = 2 * gracePeriod
		},
		wantReplicas: 0,
		wantScaling:  false,
		wantCBCount:  1,
	}, {
		label:         "can't scale to zero after grace period, but before last pod retention, pa defined",
		startReplicas: 1,
		scaleTo:       0,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
			k.Annotations[autoscaling.ScaleToZeroPodRetentionPeriodKey] = (2 * gracePeriod).String()
		},
		configMutator: func(c *config.Config) {
			c.Autoscaler.ScaleToZeroPodRetentionPeriod = 0 // Disabled in CM.
		},
		wantReplicas: 0,
		wantScaling:  false,
		wantCBCount:  1,
	}, {
		label:         "scale to zero after grace period, and after last pod retention",
		startReplicas: 1,
		scaleTo:       0,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
		},
		configMutator: func(c *config.Config) {
			c.Autoscaler.ScaleToZeroPodRetentionPeriod = gracePeriod
		},
		wantReplicas: 0,
		wantScaling:  true,
	}, {
		label:         "scale to zero after grace period",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
		},
	}, {
		label:         "waits to scale to zero (just before grace period)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod).Add(time.Second))
		},
		wantCBCount: 1,
	}, {
		label:         "waits to scale to zero (just before grace period, sks short)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod).Add(time.Second))
		},
		sks: func(s *nv1a1.ServerlessService) {
			markSKSInProxyFor(s, gracePeriod-time.Second)
		},
		wantCBCount: 1,
	}, {
		label:         "waits to scale to zero (just before grace period, sks in proxy long) and last pod timeout positive",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod).Add(time.Second))
		},
		configMutator: func(c *config.Config) {
			// This is shorter than gracePeriod=60s.
			c.Autoscaler.ScaleToZeroPodRetentionPeriod = 42 * time.Second
		},
		sks: func(s *nv1a1.ServerlessService) {
			markSKSInProxyFor(s, gracePeriod)
		},
	}, {
		label:         "waits to scale to zero (just before grace period, sks in proxy long)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod).Add(time.Second))
		},
		sks: func(s *nv1a1.ServerlessService) {
			markSKSInProxyFor(s, gracePeriod)
		},
	}, {
		label:         "scale to zero with TBC=-1",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			k.Annotations[autoscaling.TargetBurstCapacityKey] = "-1"
			paMarkInactive(k, time.Now().Add(-gracePeriod))
		},
		proberfunc: func(*autoscalingv1alpha1.PodAutoscaler, http.RoundTripper) (bool, error) {
			panic("should not be called")
		},
		wantAsyncProbeCount: 0,
	}, {
		label:         "scale to zero after grace period, but fail prober",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
		},
		proberfunc: func(*autoscalingv1alpha1.PodAutoscaler, http.RoundTripper) (bool, error) {
			return false, errors.New("hell or high water")
		},
		wantAsyncProbeCount: 1,
	}, {
		label:         "scale to zero after grace period, but wrong prober response",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
		},
		proberfunc:          func(*autoscalingv1alpha1.PodAutoscaler, http.RoundTripper) (bool, error) { return false, nil },
		wantAsyncProbeCount: 1,
	}, {
		label:         "waits to scale to zero while activating until after deadline exceeded",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  -1,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActivating(k, time.Now().Add(-activationTimeout/2))
		},
		wantCBCount: 1,
	}, {
		label:         "scale to zero while activating after deadline exceeded",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActivating(k, time.Now().Add(-(activationTimeout + time.Second)))
		},
	}, {
		label:         "scale to zero while activating after revision deadline exceeded",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			progressDeadline := "5s"
			k.Annotations[serving.ProgressDeadlineAnnotationKey] = progressDeadline
			customActivationTimeout, _ := time.ParseDuration(progressDeadline)
			paMarkActivating(k, time.Now().Add(-(customActivationTimeout + +activationTimeoutBuffer + time.Second)))
		},
	}, {
		label:         "scale down to minScale before grace period",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  2,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod+time.Second))
			WithReachabilityReachable(k)
		},
	}, {
		label:         "scale down to minScale after grace period",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  2,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
			WithReachabilityReachable(k)
		},
	}, {
		label:         "ignore minScale if unreachable",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  0,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
			WithReachabilityUnreachable(k) // not needed, here for clarity
		},
	}, {
		label:         "observe minScale if reachability unknown",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  2,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod))
			WithReachabilityUnknown(k)
		},
	}, {
		label:         "scales up",
		startReplicas: 1,
		scaleTo:       10,
		wantReplicas:  10,
		wantScaling:   true,
	}, {
		label:         "scales up to maxScale",
		startReplicas: 1,
		scaleTo:       10,
		maxScale:      8,
		wantReplicas:  8,
		wantScaling:   true,
	}, {
		label:         "scale up inactive revision",
		startReplicas: 1,
		scaleTo:       10,
		wantReplicas:  10,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now().Add(-gracePeriod/2))
		},
	}, {
		label:         "does not scale up from zero with no metrics",
		startReplicas: 0,
		scaleTo:       -1, // no metrics
		wantReplicas:  -1,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now())
		},
	}, {
		label:         "scales up from zero to desired one",
		startReplicas: 0,
		scaleTo:       1,
		wantReplicas:  1,
		wantScaling:   true,
	}, {
		label:         "scales up from zero to desired high scale",
		startReplicas: 0,
		scaleTo:       10,
		wantReplicas:  10,
		wantScaling:   true,
	}, {
		label:         "negative scale does not scale",
		startReplicas: 12,
		scaleTo:       -1,
		wantReplicas:  -1,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActive(k, time.Now())
		},
	}, {
		label:         "initial scale attained, but now time to scale down",
		startReplicas: 2,
		scaleTo:       0,
		wantReplicas:  1, // First we deactivate and scale to 1.
		wantScaling:   true,
		wantCBCount:   1,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActive(k, time.Now().Add(-2*time.Minute))
			k.Status.MarkScaleTargetInitialized()
			k.Annotations[autoscaling.InitialScaleAnnotationKey] = "2"
		},
	}, {
		label:         "haven't scaled to initial scale, override desired scale with initial scale",
		startReplicas: 0,
		scaleTo:       1,
		wantReplicas:  2,
		wantScaling:   true,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActivating(k, time.Now())
			k.Annotations[autoscaling.InitialScaleAnnotationKey] = "2"
		},
	}, {
		label:         "initial scale reached for the first time",
		startReplicas: 5,
		scaleTo:       1,
		wantReplicas:  5,
		wantScaling:   false,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkActivating(k, time.Now())
			k.Annotations[autoscaling.InitialScaleAnnotationKey] = "5"
		},
	}, {
		label:         "reaching initial scale zero",
		startReplicas: 0,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		wantCBCount:   1,
		paMutation: func(k *autoscalingv1alpha1.PodAutoscaler) {
			paMarkInactive(k, time.Now())
			k.ObjectMeta.Annotations[autoscaling.InitialScaleAnnotationKey] = "0"
		},
		configMutator: func(c *config.Config) {
			c.Autoscaler.AllowZeroInitialScale = true
		},
	}}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			ctx, _, _ := SetupFakeContextWithCancel(t, func(ctx context.Context) context.Context {
				return filteredinformerfactory.WithSelectors(ctx, serving.RevisionUID)
			})

			dynamicClient := fakedynamicclient.Get(ctx)

			revision := newRevision(ctx, t, fakeservingclient.Get(ctx), test.minScale, test.maxScale)
			deployment := newDeployment(ctx, t, dynamicClient, names.Deployment(revision), test.startReplicas)
			cbCount := 0
			revisionScaler := newScaler(ctx, podscalable.Get(ctx), func(interface{}, time.Duration) {
				cbCount++
			})
			if test.proberfunc != nil {
				revisionScaler.activatorProbe = test.proberfunc
			} else {
				revisionScaler.activatorProbe = func(*autoscalingv1alpha1.PodAutoscaler, http.RoundTripper) (bool, error) { return true, nil }
			}
			cp := &countingProber{}
			revisionScaler.probeManager = cp

			// We test like this because the dynamic client's fake doesn't properly handle
			// patch modes prior to 1.13 (where vaikas added JSON Patch support).
			gotScaling := false
			dynamicClient.PrependReactor("patch", "deployments",
				func(action clientgotesting.Action) (bool, runtime.Object, error) {
					patch := action.(clientgotesting.PatchAction)
					if !test.wantScaling {
						t.Error("Don't want scaling, but got patch:", string(patch.GetPatch()))
					}
					gotScaling = true
					return true, nil, nil
				})

			pa := newKPA(ctx, t, fakeservingclient.Get(ctx), revision)
			if test.paMutation != nil {
				test.paMutation(pa)
			}

			sks := sks("ns", "name")
			if test.sks != nil {
				test.sks(sks)
			}

			cfg := defaultConfig()
			if test.configMutator != nil {
				test.configMutator(cfg)
			}
			ctx = config.ToContext(ctx, cfg)
			desiredScale, err := revisionScaler.scale(ctx, pa, sks, test.scaleTo)
			if err != nil {
				t.Error("Scale got an unexpected error:", err)
			}
			if err == nil && desiredScale != test.wantReplicas {
				t.Errorf("desiredScale = %d, wanted %d", desiredScale, test.wantReplicas)
			}
			if got, want := cp.count, test.wantAsyncProbeCount; got != want {
				t.Errorf("Async probe invoked = %d time, want: %d", got, want)
			}
			if got, want := cbCount, test.wantCBCount; got != want {
				t.Errorf("Enqueue callback invoked = %d time, want: %d", got, want)
			}
			if test.wantScaling {
				if !gotScaling {
					t.Error("want scaling, but got no scaling")
				}
				checkReplicas(t, dynamicClient, deployment, test.wantReplicas)
			}
		})
	}
}

func TestDisableScaleToZero(t *testing.T) {
	tests := []struct {
		label         string
		startReplicas int
		scaleTo       int32
		minScale      int32
		maxScale      int32
		wantReplicas  int32
		wantScaling   bool
	}{{
		label:         "EnableScaleToZero == false and minScale == 0",
		startReplicas: 10,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   true,
	}, {
		label:         "EnableScaleToZero == false and minScale == 2",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  2,
		wantScaling:   true,
	}, {
		label:         "EnableScaleToZero == false and desire pod is -1(initial value)",
		startReplicas: 10,
		scaleTo:       -1,
		wantReplicas:  -1,
		wantScaling:   false,
	}}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			ctx, _, _ := SetupFakeContextWithCancel(t, func(ctx context.Context) context.Context {
				return filteredinformerfactory.WithSelectors(ctx, serving.RevisionUID)
			})

			dynamicClient := fakedynamicclient.Get(ctx)

			// We test like this because the dynamic client's fake doesn't properly handle
			// patch modes prior to 1.13 (where vaikas added JSON Patch support).
			gotScaling := false
			dynamicClient.PrependReactor("patch", "deployments",
				func(action clientgotesting.Action) (bool, runtime.Object, error) {
					patch := action.(clientgotesting.PatchAction)
					if !test.wantScaling {
						t.Error("don't want scaling, but got patch:", string(patch.GetPatch()))
					}
					gotScaling = true
					return true, nil, nil
				})

			revision := newRevision(ctx, t, fakeservingclient.Get(ctx), test.minScale, test.maxScale)
			deployment := newDeployment(ctx, t, dynamicClient, names.Deployment(revision), test.startReplicas)
			psInformerFactory := podscalable.Get(ctx)
			revisionScaler := &scaler{
				dynamicClient: fakedynamicclient.Get(ctx),
				listerFactory: func(gvr schema.GroupVersionResource) (cache.GenericLister, error) {
					_, l, err := psInformerFactory.Get(ctx, gvr)
					return l, err
				},
			}
			pa := newKPA(ctx, t, fakeservingclient.Get(ctx), revision)
			paMarkActive(pa, time.Now())
			WithReachabilityReachable(pa)

			conf := defaultConfig()
			conf.Autoscaler.EnableScaleToZero = false
			ctx = config.ToContext(ctx, conf)
			desiredScale, err := revisionScaler.scale(ctx, pa, nil /*sks doesn't matter in this test*/, test.scaleTo)

			if err != nil {
				t.Error("Scale got an unexpected error:", err)
			}
			if err == nil && desiredScale != test.wantReplicas {
				t.Errorf("desiredScale = %d, wanted %d", desiredScale, test.wantReplicas)
			}
			if test.wantScaling {
				if !gotScaling {
					t.Error("want scaling, but got no scaling")
				}
				checkReplicas(t, dynamicClient, deployment, test.wantReplicas)
			}
		})
	}
}

func newKPA(ctx context.Context, t *testing.T, servingClient clientset.Interface, revision *v1.Revision) *autoscalingv1alpha1.PodAutoscaler {
	t.Helper()
	pa := revisionresources.MakePA(revision, nil)
	pa.Status.InitializeConditions()
	_, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(ctx, pa, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create PA:", err)
	}
	return pa
}

func newRevision(ctx context.Context, t *testing.T, servingClient clientset.Interface, minScale, maxScale int32) *v1.Revision {
	t.Helper()
	annotations := map[string]string{}
	if minScale > 0 {
		annotations[autoscaling.MinScaleAnnotationKey] = strconv.Itoa(int(minScale))
	}
	if maxScale > 0 {
		annotations[autoscaling.MaxScaleAnnotationKey] = strconv.Itoa(int(maxScale))
	}
	rev := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   testNamespace,
			Name:        testRevision,
			Annotations: annotations,
		},
	}
	rev, err := servingClient.ServingV1().Revisions(testNamespace).Create(ctx, rev, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create revision:", err)
	}

	return rev
}

func newDeployment(ctx context.Context, t *testing.T, dynamicClient dynamic.Interface, name string, replicas int) *appsv1.Deployment {
	t.Helper()

	uns := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"namespace": testNamespace,
				"name":      name,
				"uid":       "1982",
			},
			"spec": map[string]interface{}{
				"replicas": int64(replicas),
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						serving.RevisionUID: "1982",
					},
				},
			},
			"status": map[string]interface{}{
				"replicas": int64(replicas),
			},
		},
	}

	u, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}).Namespace(testNamespace).Create(ctx, uns, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Create() =", err)
	}

	deployment := &appsv1.Deployment{}
	if err := duck.FromUnstructured(u, deployment); err != nil {
		t.Fatal("FromUnstructured() =", err)
	}
	return deployment
}

func paMarkActive(pa *autoscalingv1alpha1.PodAutoscaler, ltt time.Time) {
	pa.Status.MarkActive()

	// This works because the conditions are sorted alphabetically
	pa.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(ltt)}
}

func paMarkInactive(pa *autoscalingv1alpha1.PodAutoscaler, ltt time.Time) {
	pa.Status.MarkInactive("", "")

	// This works because the conditions are sorted alphabetically
	pa.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(ltt)}
}

func paMarkActivating(pa *autoscalingv1alpha1.PodAutoscaler, ltt time.Time) {
	pa.Status.MarkActivating("", "")

	// This works because the conditions are sorted alphabetically
	pa.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(ltt)}
}

func checkReplicas(t *testing.T, dynamicClient *fakedynamic.FakeDynamicClient, deployment *appsv1.Deployment, expectedScale int32) {
	t.Helper()

	found := false
	for _, action := range dynamicClient.Actions() {
		if action.GetVerb() == "patch" {
			patch := action.(clientgotesting.PatchAction)
			if patch.GetName() != deployment.Name {
				continue
			}
			want := fmt.Sprintf(`[{"op":"replace","path":"/spec/replicas","value":%d}]`, expectedScale)
			if got := string(patch.GetPatch()); got != want {
				t.Errorf("Patch = %s, wanted %s", got, want)
			}
			found = true
		}
	}

	if !found {
		t.Errorf("Did not see scale update for %q", deployment.Name)
	}
}

func TestActivatorProbe(t *testing.T) {
	oldRT := network.AutoTransport
	defer func() {
		network.AutoTransport = oldRT
	}()
	theErr := errors.New("rain")

	pa := kpa("who-let", "the-dogs-out", WithPAStatusService("woof"))
	tests := []struct {
		name    string
		rt      network.RoundTripperFunc
		wantRes bool
		wantErr bool
	}{{
		name: "ok",
		rt: func(r *http.Request) (*http.Response, error) {
			rsp := httptest.NewRecorder()
			rsp.Write([]byte(activator.Name))
			return rsp.Result(), nil
		},
		wantRes: true,
	}, {
		name: "400",
		rt: func(r *http.Request) (*http.Response, error) {
			rsp := httptest.NewRecorder()
			rsp.Code = http.StatusBadRequest
			rsp.Write([]byte("wrong header, I guess?"))
			return rsp.Result(), nil
		},
		wantErr: true,
	}, {
		name: "wrong body",
		rt: func(r *http.Request) (*http.Response, error) {
			rsp := httptest.NewRecorder()
			rsp.Write([]byte("haxoorprober"))
			return rsp.Result(), nil
		},
		wantErr: true,
	}, {
		name: "all wrong",
		rt: func(r *http.Request) (*http.Response, error) {
			return nil, theErr
		},
		wantErr: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := activatorProbe(pa, test.rt)
			if got, want := res, test.wantRes; got != want {
				t.Errorf("Result = %v, want: %v", got, want)
			}
			if got, want := err != nil, test.wantErr; got != want {
				t.Errorf("WantErr = %v, want: %v: actual error is: %v", got, want, err)
			}
		})
	}
}

type countingProber struct {
	count int
}

func (c *countingProber) Offer(ctx context.Context, target string, arg interface{}, period, timeout time.Duration, ops ...interface{}) bool {
	c.count++
	return true
}

func markSKSInProxyFor(sks *nv1a1.ServerlessService, d time.Duration) {
	sks.Status.MarkActivatorEndpointsPopulated()
	// This works because the conditions are sorted alphabetically
	sks.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(time.Now().Add(-d))}
}
