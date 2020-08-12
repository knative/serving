/*
Copyright 2018 The Knative Authors.

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
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	network "knative.dev/networking/pkg"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	"knative.dev/serving/pkg/deployment"

	. "knative.dev/pkg/reconciler/testing"
)

type nopResolver struct{}

func (r *nopResolver) Resolve(context.Context, string, k8schain.Options, sets.String) (string, error) {
	return "", nil
}

const (
	testAutoscalerImage = "autoscalerImage"
	testNamespace       = "test"
	testQueueImage      = "queueImage"
)

func getPodSpec() corev1.PodSpec {
	return corev1.PodSpec{
		// corev1.Container has a lot of setting.  We try to pass many
		// of them here to verify that we pass through the settings to
		// derived objects.
		Containers: []corev1.Container{{
			Image:      "gcr.io/repo/image",
			Command:    []string{"echo"},
			Args:       []string{"hello", "world"},
			WorkingDir: "/tmp",
			Env: []corev1.EnvVar{{
				Name:  "EDITOR",
				Value: "emacs",
			}},
			LivenessProbe: &corev1.Probe{
				TimeoutSeconds: 42,
			},
			ReadinessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "health",
					},
				},
				TimeoutSeconds: 43,
			},
			TerminationMessagePath: "/dev/null",
		}},
	}
}

func testRevision(podSpec corev1.PodSpec) *v1.Revision {
	rev := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1/namespaces/test/revisions/test-rev",
			Name:      "test-rev",
			Namespace: testNamespace,
			Labels: map[string]string{
				"testLabel1":          "foo",
				"testLabel2":          "bar",
				serving.RouteLabelKey: "test-route",
			},
			Annotations: map[string]string{
				"testAnnotation": "test",
			},
			UID: "test-rev-uid",
		},
		Spec: v1.RevisionSpec{
			PodSpec:        podSpec,
			TimeoutSeconds: ptr.Int64(60),
		},
	}
	rev.SetDefaults(context.Background())
	return rev
}

func getTestDeploymentConfig() *deployment.Config {
	c, _ := deployment.NewConfigFromConfigMap(getTestDeploymentConfigMap())
	// ignoring error as test controller is generated
	return c
}

func getTestDeploymentConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployment.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"queueSidecarImage": testQueueImage,
			"autoscalerImage":   testAutoscalerImage,
		},
	}
}

func getTestDefaultsConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.DefaultsConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"container-name-template": "user-container",
		},
	}
}

func newTestController(t *testing.T, opts ...reconcilerOption) (
	context.Context,
	context.CancelFunc,
	[]controller.Informer,
	*controller.Impl,
	*configmap.ManualWatcher) {

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}

	// Prepend so that callers can override.
	opts = append([]reconcilerOption{func(r *Reconciler) {
		r.resolver = &nopResolver{}
	}}, opts...)
	controller := newControllerWithOptions(ctx, configMapWatcher, opts...)

	configs := []*corev1.ConfigMap{
		getTestDeploymentConfigMap(),
		getTestDefaultsConfigMap(),
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      network.ConfigName,
			}}, {
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      config.FeaturesConfigName,
			},
			Data: map[string]string{},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      logging.ConfigMapName(),
			},
			Data: map[string]string{
				"zap-logger-config":   "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}",
				"loglevel.queueproxy": "info",
			}}, {
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      tracingconfig.ConfigName,
			},
			Data: map[string]string{
				"enable":          "true",
				"debug":           "true",
				"zipkin-endpoint": "http://zipkin.istio-system.svc.cluster.local:9411/api/v2/spans",
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      metrics.ConfigMapName(),
			},
			Data: map[string]string{
				"logging.enable-var-log-collection": "true",
			}}, {
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      autoscalerconfig.ConfigName,
			},
			Data: map[string]string{
				"max-scale-up-rate":                       "2.0",
				"container-concurrency-target-percentage": "0.5",
				"container-concurrency-target-default":    "10.0",
				"stable-window":                           "5m",
				"panic-window":                            "10s",
				"scale-to-zero-threshold":                 "10m",
				"tick-interval":                           "2s",
			}},
	}
	for _, configMap := range configs {
		configMapWatcher.OnChange(configMap)
	}

	// The Reconciler won't do any work until it becomes the leader.
	if la, ok := controller.Reconciler.(reconciler.LeaderAware); ok {
		la.Promote(reconciler.UniversalBucket(), func(reconciler.Bucket, types.NamespacedName) {})
	}

	return ctx, cancel, informers, controller, configMapWatcher
}

func TestNewRevisionCallsSyncHandler(t *testing.T) {
	ctx, cancel, informers, ctrl, _ := newTestController(t)
	// Create tracer with reporter recorder
	reporter, co := tracetesting.FakeZipkinExporter()
	defer reporter.Close()
	oct := tracing.NewOpenCensusTracer(co)
	defer oct.Finish()

	cfg := tracingconfig.Config{
		Backend: tracingconfig.Zipkin,
		Debug:   true,
	}
	if err := oct.ApplyConfig(&cfg); err != nil {
		t.Errorf("Failed to apply tracer config: %v", err)
	}

	eg := errgroup.Group{}

	rev := testRevision(getPodSpec())
	servingClient := fakeservingclient.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Error starting informers:", err)
	}
	defer func() {
		cancel()
		if err := eg.Wait(); err != nil {
			t.Fatal("Error running controller:", err)
		}
		waitInformers()
	}()

	eg.Go(func() error {
		return ctrl.Run(2, ctx.Done())
	})

	if _, err := servingClient.ServingV1().Revisions(rev.Namespace).Create(rev); err != nil {
		t.Fatal("Error creating revision:", err)
	}

	// Poll to see PA object to be created.
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		pa, _ := servingClient.AutoscalingV1alpha1().PodAutoscalers(rev.Namespace).Get(
			rev.Name, metav1.GetOptions{})
		return pa != nil, nil
	}); err != nil {
		t.Error("Failed to see PA creation")
	}
}
