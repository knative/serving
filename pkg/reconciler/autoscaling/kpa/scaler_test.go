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
	fakedynamicclient "github.com/knative/pkg/injection/clients/dynamicclient/fake"
	fakeservingclient "github.com/knative/serving/pkg/client/injection/client/fake"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/apis/duck"
	"github.com/knative/pkg/logging"
	logtesting "github.com/knative/pkg/logging/testing"
	_ "github.com/knative/pkg/system/testing"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/autoscaling"
	pav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/autoscaling/config"
	revisionresources "github.com/knative/serving/pkg/reconciler/revision/resources"
	"github.com/knative/serving/pkg/reconciler/revision/resources/names"
	presources "github.com/knative/serving/pkg/resources"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/pkg/reconciler/testing"
	. "github.com/knative/serving/pkg/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestScaler(t *testing.T) {
	defer logtesting.ClearAll()
	tests := []struct {
		label               string
		startReplicas       int
		scaleTo             int32
		minScale            int32
		maxScale            int32
		wantReplicas        int32
		wantScaling         bool
		kpaMutation         func(*pav1alpha1.PodAutoscaler)
		proberfunc          func(*pav1alpha1.PodAutoscaler, http.RoundTripper) (bool, error)
		wantCBCount         int
		wantAsyncProbeCount int
	}{{
		label:         "waits to scale to zero (just before idle period)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkActive(k, time.Now().Add(-stableWindow).Add(1*time.Second))
		},
		wantCBCount: 1,
	}, {
		label:         "scale to 1 waiting for idle expires",
		startReplicas: 10,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   true,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkActive(k, time.Now().Add(-stableWindow).Add(1*time.Second))
		},
		wantCBCount: 1,
	}, {
		label:         "waits to scale to zero after idle period",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkActive(k, time.Now().Add(-stableWindow))
		},
	}, {
		label:         "scale to zero after grace period",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod))
		},
	}, {
		label:         "waits to scale to zero (just before grace period)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod).Add(1*time.Second))
		},
		wantCBCount: 1,
	}, {
		label:         "scale to zero after grace period, but fail prober",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod))
		},
		proberfunc: func(*pav1alpha1.PodAutoscaler, http.RoundTripper) (bool, error) {
			return false, errors.New("hell or high water")
		},
		wantAsyncProbeCount: 1,
	}, {
		label:         "scale to zero after grace period, but wrong prober response",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod))
		},
		proberfunc:          func(*pav1alpha1.PodAutoscaler, http.RoundTripper) (bool, error) { return false, nil },
		wantAsyncProbeCount: 1,
	}, {
		label:         "does not scale while activating",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  -1,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			k.Status.MarkActivating("", "")
		},
	}, {
		label:         "scale down to minScale before grace period",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  2,
		wantScaling:   true,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod+time.Second))
		},
	}, {
		label:         "scale down to minScale after grace period",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  2,
		wantScaling:   true,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod))
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
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod/2))
		},
	}, {
		label:         "does not scale up from zero with no metrics",
		startReplicas: 0,
		scaleTo:       -1, // no metrics
		wantReplicas:  -1,
		wantScaling:   false,
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
	}}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			ctx, _ := SetupFakeContext(t)

			dynamicClient := fakedynamicclient.Get(ctx)

			revision := newRevision(t, fakeservingclient.Get(ctx), test.minScale, test.maxScale)
			deployment := newDeployment(t, dynamicClient, names.Deployment(revision), test.startReplicas)
			cbCount := 0
			revisionScaler := newScaler(ctx, presources.NewPodScalableInformerFactory(ctx), func(interface{}, time.Duration) {
				cbCount++
			})
			if test.proberfunc != nil {
				revisionScaler.activatorProbe = test.proberfunc
			} else {
				revisionScaler.activatorProbe = func(*pav1alpha1.PodAutoscaler, http.RoundTripper) (bool, error) { return true, nil }
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
						t.Errorf("don't want scaling, but got patch: %s", string(patch.GetPatch()))
					}
					gotScaling = true
					return true, nil, nil
				})

			pa := newKPA(t, fakeservingclient.Get(ctx), revision)
			if test.kpaMutation != nil {
				test.kpaMutation(pa)
			}

			ctx = config.ToContext(ctx, defaultConfig())
			desiredScale, err := revisionScaler.Scale(ctx, pa, test.scaleTo)
			if err != nil {
				t.Error("Scale got an unexpected error: ", err)
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
	defer logtesting.ClearAll()
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
			ctx, _ := SetupFakeContext(t)

			dynamicClient := fakedynamicclient.Get(ctx)

			// We test like this because the dynamic client's fake doesn't properly handle
			// patch modes prior to 1.13 (where vaikas added JSON Patch support).
			gotScaling := false
			dynamicClient.PrependReactor("patch", "deployments",
				func(action clientgotesting.Action) (bool, runtime.Object, error) {
					patch := action.(clientgotesting.PatchAction)
					if !test.wantScaling {
						t.Errorf("don't want scaling, but got patch: %s", string(patch.GetPatch()))
					}
					gotScaling = true
					return true, nil, nil
				})

			revision := newRevision(t, fakeservingclient.Get(ctx), test.minScale, test.maxScale)
			deployment := newDeployment(t, dynamicClient, names.Deployment(revision), test.startReplicas)
			revisionScaler := &scaler{
				dynamicClient:     fakedynamicclient.Get(ctx),
				logger:            logging.FromContext(ctx),
				psInformerFactory: presources.NewPodScalableInformerFactory(ctx),
			}
			pa := newKPA(t, fakeservingclient.Get(ctx), revision)

			conf := defaultConfig()
			conf.Autoscaler.EnableScaleToZero = false
			ctx = config.ToContext(ctx, conf)
			desiredScale, err := revisionScaler.Scale(ctx, pa, test.scaleTo)

			if err != nil {
				t.Error("Scale got an unexpected error: ", err)
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

func newKPA(t *testing.T, servingClient clientset.Interface, revision *v1alpha1.Revision) *pav1alpha1.PodAutoscaler {
	pa := revisionresources.MakeKPA(revision)
	pa.Status.InitializeConditions()
	_, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(pa)
	if err != nil {
		t.Fatal("Failed to create PA.", err)
	}
	return pa
}

func newRevision(t *testing.T, servingClient clientset.Interface, minScale, maxScale int32) *v1alpha1.Revision {
	annotations := map[string]string{}
	if minScale > 0 {
		annotations[autoscaling.MinScaleAnnotationKey] = strconv.Itoa(int(minScale))
	}
	if maxScale > 0 {
		annotations[autoscaling.MaxScaleAnnotationKey] = strconv.Itoa(int(maxScale))
	}
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   testNamespace,
			Name:        testRevision,
			Annotations: annotations,
		},
		Spec: v1alpha1.RevisionSpec{
			DeprecatedConcurrencyModel: "Multi",
		},
	}
	rev, err := servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	if err != nil {
		t.Fatal("Failed to create revision.", err)
	}

	return rev
}

func newDeployment(t *testing.T, dynamicClient dynamic.Interface, name string, replicas int) *v1.Deployment {
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
	}).Namespace(testNamespace).Create(uns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	deployment := &v1.Deployment{}
	if err := duck.FromUnstructured(u, deployment); err != nil {
		t.Fatalf("FromUnstructured() = %v", err)
	}
	return deployment
}

func kpaMarkActive(pa *pav1alpha1.PodAutoscaler, ltt time.Time) {
	pa.Status.MarkActive()

	// This works because the conditions are sorted alphabetically
	pa.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(ltt)}
}

func kpaMarkInactive(pa *pav1alpha1.PodAutoscaler, ltt time.Time) {
	pa.Status.MarkInactive("", "")

	// This works because the conditions are sorted alphabetically
	pa.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(ltt)}
}

func checkReplicas(t *testing.T, dynamicClient *fakedynamic.FakeDynamicClient, deployment *v1.Deployment, expectedScale int32) {
	t.Helper()

	found := false
	for _, action := range dynamicClient.Actions() {
		switch action.GetVerb() {
		case "patch":
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
		t.Errorf("Did not see scale update for %v", deployment.Name)
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
		wantRes: false,
	}, {
		name: "wrong body",
		rt: func(r *http.Request) (*http.Response, error) {
			rsp := httptest.NewRecorder()
			rsp.Write([]byte("haxoorprober"))
			return rsp.Result(), nil
		},
		wantRes: false,
	}, {
		name: "all wrong",
		rt: func(r *http.Request) (*http.Response, error) {
			return nil, theErr
		},
		wantRes: false,
		wantErr: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := activatorProbe(pa, test.rt)
			if got, want := res, test.wantRes; got != want {
				t.Errorf("Result = %v, want: %v", got, want)
			}
			if got, want := err != nil, test.wantErr; got != want {
				t.Errorf("WantErr = %v, want: %v", got, want)
			}
		})
	}
}

type countingProber struct {
	count int
}

func (c *countingProber) Offer(ctx context.Context, target, headerValue string, arg interface{}, period, timeout time.Duration) bool {
	c.count++
	return true
}
