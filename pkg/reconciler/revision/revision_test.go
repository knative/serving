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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"knative.dev/serving/pkg/apis/config"

	// Inject the fakes for informers this controller relies on.
	fakecachingclient "knative.dev/caching/pkg/client/injection/client/fake"
	fakeimageinformer "knative.dev/caching/pkg/client/injection/informers/caching/v1alpha1/image/fake"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakedeploymentinformer "knative.dev/pkg/client/injection/kube/informers/apps/v1/deployment/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/configmap/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakepainformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/pkg/system"
	tracingconfig "knative.dev/pkg/tracing/config"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/revision/resources"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"

	. "knative.dev/pkg/reconciler/testing"
)

func testConfiguration() *v1.Configuration {
	return &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1/namespaces/test/configurations/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
	}
}

func serviceName(rn string) string {
	return rn
}

func testReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName(revName),
			Namespace: testNamespace,
			Labels: map[string]string{
				serving.RevisionLabelKey: revName,
			},
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				IP: "123.456.78.90",
			}},
		}},
	}
}

func testReadyPA(rev *v1.Revision) *av1alpha1.PodAutoscaler {
	pa := resources.MakePA(rev)
	pa.Status.InitializeConditions()
	pa.Status.MarkActive()
	pa.Status.ServiceName = serviceName(rev.Name)
	return pa
}

func newTestControllerWithConfig(t *testing.T, deploymentConfig *deployment.Config, configs []*corev1.ConfigMap, opts ...reconcilerOption) (
	context.Context,
	[]controller.Informer,
	*controller.Impl,
	*configmap.ManualWatcher) {

	ctx, informers := SetupFakeContext(t)
	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}

	// Prepend so that callers can override.
	opts = append([]reconcilerOption{func(r *Reconciler) {
		r.resolver = &nopResolver{}
	}}, opts...)

	controller := newControllerWithOptions(ctx, configMapWatcher, opts...)

	cms := []*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      network.ConfigName,
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      logging.ConfigMapName(),
		},
		Data: map[string]string{
			"zap-logger-config":   "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}",
			"loglevel.queueproxy": "info",
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      metrics.ConfigMapName(),
		},
		Data: map[string]string{
			"logging.enable-var-log-collection": "true",
		},
	}, {
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
			Name:      autoscalerconfig.ConfigName,
		},
		Data: map[string]string{
			"max-scale-up-rate":                       "11.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
		},
	}, getTestDeploymentConfigMap(), getTestDefaultsConfigMap()}

	cms = append(cms, configs...)

	for _, configMap := range cms {
		configMapWatcher.OnChange(configMap)
	}
	return ctx, informers, controller, configMapWatcher
}

func createRevision(
	t *testing.T,
	ctx context.Context,
	controller *controller.Impl,
	rev *v1.Revision,
) *v1.Revision {
	t.Helper()
	fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Create(rev)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	if err := controller.Reconciler.Reconcile(context.Background(), KeyOrDie(rev)); err == nil {
		rev, _, _ = addResourcesToInformers(t, ctx, rev)
	}
	return rev
}

func updateRevision(
	t *testing.T,
	ctx context.Context,
	controller *controller.Impl,
	rev *v1.Revision,
) {
	t.Helper()
	fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Update(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Update(rev)

	if err := controller.Reconciler.Reconcile(context.Background(), KeyOrDie(rev)); err == nil {
		addResourcesToInformers(t, ctx, rev)
	}
}

func addResourcesToInformers(t *testing.T, ctx context.Context, rev *v1.Revision) (*v1.Revision, *appsv1.Deployment, *av1alpha1.PodAutoscaler) {
	t.Helper()

	rev, err := fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Revisions.Get(%v) = %v", rev.Name, err)
	}
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	ns := rev.Namespace

	paName := resourcenames.PA(rev)
	pa, err := fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(rev.Namespace).Get(paName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("PodAutoscalers.Get(%v) = %v", paName, err)
	} else {
		fakepainformer.Get(ctx).Informer().GetIndexer().Add(pa)
	}

	imageName := resourcenames.ImageCache(rev)
	image, err := fakecachingclient.Get(ctx).CachingV1alpha1().Images(rev.Namespace).Get(imageName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Caching.Images.Get(%v) = %v", imageName, err)
	} else {
		fakeimageinformer.Get(ctx).Informer().GetIndexer().Add(image)
	}

	deploymentName := resourcenames.Deployment(rev)
	deployment, err := fakekubeclient.Get(ctx).AppsV1().Deployments(ns).Get(deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Deployments.Get(%v) = %v", deploymentName, err)
	} else {
		fakedeploymentinformer.Get(ctx).Informer().GetIndexer().Add(deployment)
	}

	return rev, deployment, pa
}

type errorResolver struct {
	err error
}

func (r *errorResolver) Resolve(_ string, _ k8schain.Options, _ sets.String) (string, error) {
	return "", r.err
}

func TestResolutionFailed(t *testing.T) {
	// Unconditionally return this error during resolution.
	innerError := errors.New("i am the expected error message, hear me ROAR!")
	ctx, cancel, _, controller, _ := newTestController(t, func(r *Reconciler) {
		r.resolver = &errorResolver{innerError}
	})
	defer cancel()

	rev := testRevision(getPodSpec())
	config := testConfiguration()
	rev.OwnerReferences = append(rev.OwnerReferences, *kmeta.NewControllerRef(config))

	createRevision(t, ctx, controller, rev)

	rev, err := fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Ensure that the Revision status is updated.
	for _, ct := range []apis.ConditionType{"ContainerHealthy", "Ready"} {
		got := rev.Status.GetCondition(ct)
		want := &apis.Condition{
			Type:   ct,
			Status: corev1.ConditionFalse,
			Reason: "ContainerMissing",
			Message: v1.RevisionContainerMissingMessage(
				rev.Spec.GetContainer().Image, "failed to resolve image to digest: "+innerError.Error()),
			LastTransitionTime: got.LastTransitionTime,
			Severity:           apis.ConditionSeverityError,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}
}

// TODO(mattmoor): add coverage of a Reconcile fixing a stale logging URL
func TestUpdateRevWithWithUpdatedLoggingURL(t *testing.T) {
	deploymentConfig := getTestDeploymentConfig()
	ctx, _, controller, watcher := newTestControllerWithConfig(t, deploymentConfig, []*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      metrics.ConfigMapName(),
		},
		Data: map[string]string{
			"logging.enable-var-log-collection": "true",
			"logging.revision-url-template":     "http://old-logging.test.com?filter=${REVISION_UID}",
		},
	}, getTestDeploymentConfigMap(),
	})
	revClient := fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace)

	rev := testRevision(getPodSpec())
	createRevision(t, ctx, controller, rev)

	// Update controllers logging URL
	watcher.OnChange(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      metrics.ConfigMapName(),
		},
		Data: map[string]string{
			"logging.enable-var-log-collection": "true",
			"logging.revision-url-template":     "http://new-logging.test.com?filter=${REVISION_UID}",
		},
	})
	updateRevision(t, ctx, controller, rev)

	updatedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	expectedLoggingURL := fmt.Sprintf("http://new-logging.test.com?filter=%s", rev.UID)
	if updatedRev.Status.LogURL != expectedLoggingURL {
		t.Errorf("Updated revision does not have an updated logging URL: expected: %s, got: %s", expectedLoggingURL, updatedRev.Status.LogURL)
	}
}

func TestRevWithImageDigests(t *testing.T) {
	deploymentConfig := getTestDeploymentConfig()
	ctx, _, controller, _ := newTestControllerWithConfig(t, deploymentConfig, []*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.DefaultsConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"container-name-template": "user-container",
		},
	}})

	rev := testRevision(corev1.PodSpec{
		Containers: []corev1.Container{{
			Image: "gcr.io/repo/image",
			Ports: []corev1.ContainerPort{{
				ContainerPort: 8888,
			}},
		}, {
			Image: "docker.io/repo/image",
		}},
	})
	createRevision(t, ctx, controller, rev)
	revClient := fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace)
	rev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}
	if len(rev.Status.ImageDigests) < 2 {
		t.Error("Revision status does not have imageDigests")
	}

	rev.Status.DeprecatedImageDigest = "gcr.io/repo/image"
	updateRevision(t, ctx, controller, rev)
	if len(rev.Spec.Containers) != len(rev.Status.ImageDigests) {
		t.Error("Image digests does not match with the provided containers")
	}
	rev.Status.ImageDigests = map[string]string{}
	updateRevision(t, ctx, controller, rev)
	if len(rev.Status.ImageDigests) != 0 {
		t.Error("Failed to update revision")
	}
}

// TODO(mattmoor): Remove when we have coverage of EnqueueEndpointsRevision
func TestMarkRevReadyUponEndpointBecomesReady(t *testing.T) {
	ctx, cancel, _, ctl, _ := newTestController(t)
	defer cancel()
	rev := testRevision(getPodSpec())

	fakeRecorder := controller.GetEventRecorder(ctx).(*record.FakeRecorder)

	// Look for the revision ready event. Events are delivered asynchronously so
	// we need to use hooks here.

	deployingRev := createRevision(t, ctx, ctl, rev)

	// The revision is not marked ready until an endpoint is created.
	for _, ct := range []apis.ConditionType{"Ready"} {
		got := deployingRev.Status.GetCondition(ct)
		want := &apis.Condition{
			Type:               ct,
			Status:             corev1.ConditionUnknown,
			Reason:             "Deploying",
			LastTransitionTime: got.LastTransitionTime,
			Severity:           apis.ConditionSeverityError,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}

	endpoints := testReadyEndpoints(rev.Name)
	fakeendpointsinformer.Get(ctx).Informer().GetIndexer().Add(endpoints)
	pa := testReadyPA(rev)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(pa)
	f := ctl.EnqueueLabelOfNamespaceScopedResource("", serving.RevisionLabelKey)
	f(endpoints)
	if err := ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(rev)); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	// Make sure that the changes from the Reconcile are reflected in our Informers.
	readyRev, _, _ := addResourcesToInformers(t, ctx, rev)

	// After reconciling the endpoint, the revision should be ready.
	for _, ct := range []apis.ConditionType{"Ready"} {
		got := readyRev.Status.GetCondition(ct)
		want := &apis.Condition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: got.LastTransitionTime,
			Severity:           apis.ConditionSeverityError,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}

	select {
	case got := <-fakeRecorder.Events:
		const want = "Normal RevisionReady Revision becomes ready upon all resources being ready"
		if got != want {
			t.Errorf("<-Events = %s, wanted %s", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Error("Timeout")
	}
}

func TestNoQueueSidecarImageUpdateFail(t *testing.T) {
	ctx, cancel, _, controller, watcher := newTestController(t)
	defer cancel()

	rev := testRevision(getPodSpec())
	config := testConfiguration()
	rev.OwnerReferences = append(
		rev.OwnerReferences,
		*kmeta.NewControllerRef(config),
	)
	// Update controller config with no side car image
	watcher.OnChange(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config-controller",
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	})
	createRevision(t, ctx, controller, rev)

	// Look for the revision deployment.
	_, err := fakekubeclient.Get(ctx).AppsV1().Deployments(system.Namespace()).Get(rev.Name, metav1.GetOptions{})
	if !apierrs.IsNotFound(err) {
		t.Errorf("Expected revision deployment %s to not exist.", rev.Name)
	}
}

func TestGlobalResyncOnConfigMapUpdateRevision(t *testing.T) {
	// Test that changes to the ConfigMap result in the desired changes on an existing
	// revision.
	tests := []struct {
		name              string
		configMapToUpdate *corev1.ConfigMap
		callback          func(*testing.T) func(runtime.Object) HookResult
	}{{
		name: "Update LoggingURL", // Should update LogURL on revision
		configMapToUpdate: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      metrics.ConfigMapName(),
			},
			Data: map[string]string{
				"logging.enable-var-log-collection": "true",
				"logging.revision-url-template":     "http://log-here.test.com?filter=${REVISION_UID}",
			},
		},
		callback: func(t *testing.T) func(runtime.Object) HookResult {
			return func(obj runtime.Object) HookResult {
				revision := obj.(*v1.Revision)
				t.Logf("Revision updated: %v", revision.Name)

				const expected = "http://log-here.test.com?filter="
				got := revision.Status.LogURL
				if strings.HasPrefix(got, expected) {
					return HookComplete
				}

				t.Logf("No update occurred; expected: %s got: %s", expected, got)
				return HookIncomplete
			}
		},
	}, {
		name: "Update ContainerConcurrency", // Should update ContainerConcurrency on revision spec
		configMapToUpdate: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      config.DefaultsConfigName,
			},
			Data: map[string]string{
				"container-concurrency": "3",
			},
		},
		callback: func(t *testing.T) func(runtime.Object) HookResult {
			return func(obj runtime.Object) HookResult {
				revision := obj.(*v1.Revision)
				t.Logf("Revision updated: %v", revision.Name)

				expected := int64(3)
				got := *(revision.Spec.ContainerConcurrency)
				if got != expected {
					return HookComplete
				}

				t.Logf("No update occurred; expected: %d got: %d", expected, got)
				return HookIncomplete
			}
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controllerConfig := getTestDeploymentConfig()
			ctx, informers, ctrl, watcher := newTestControllerWithConfig(t, controllerConfig, nil)

			ctx, cancel := context.WithCancel(ctx)
			grp := errgroup.Group{}

			servingClient := fakeservingclient.Get(ctx)

			rev := testRevision(getPodSpec())
			revClient := servingClient.ServingV1().Revisions(rev.Namespace)

			h := NewHooks()

			h.OnUpdate(&servingClient.Fake, "revisions", test.callback(t))

			waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
			if err != nil {
				t.Fatalf("Failed to start informers: %v", err)
			}
			defer func() {
				cancel()
				if err := grp.Wait(); err != nil {
					t.Errorf("Wait() = %v", err)
				}
				waitInformers()
			}()

			if err := watcher.Start(ctx.Done()); err != nil {
				t.Fatalf("Failed to start configuration manager: %v", err)
			}

			grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

			revClient.Create(rev)
			revL := fakerevisioninformer.Get(ctx).Lister()
			if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
				l, err := revL.List(labels.Everything())
				if err != nil {
					return false, err
				}
				// We only create a single revision.
				return len(l) > 0, nil
			}); err != nil {
				t.Fatalf("Failed to see Revision propagation: %v", err)
			}
			t.Log("Seen revision propagation")

			watcher.OnChange(test.configMapToUpdate)

			if err := h.WaitForHooks(3 * time.Second); err != nil {
				t.Errorf("Global Resync Failed: %v", err)
			}
		})
	}
}

func TestGlobalResyncOnConfigMapUpdateDeployment(t *testing.T) {
	// Test that changes to the ConfigMap result in the desired changes on an existing
	// deployment.
	tests := []struct {
		name              string
		configMapToUpdate *corev1.ConfigMap
		callback          func(*testing.T) func(runtime.Object) HookResult
	}{{
		name: "Disable /var/log Collection", // Should set ENABLE_VAR_LOG_COLLECTION to false
		configMapToUpdate: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      metrics.ConfigMapName(),
			},
			Data: map[string]string{
				"logging.enable-var-log-collection": "false",
			},
		},
		callback: func(t *testing.T) func(runtime.Object) HookResult {
			return func(obj runtime.Object) HookResult {
				deployment := obj.(*appsv1.Deployment)
				t.Logf("Deployment updated: %v", deployment.Name)

				for _, c := range deployment.Spec.Template.Spec.Containers {
					if c.Name == resources.QueueContainerName {
						for _, e := range c.Env {
							if e.Name == "ENABLE_VAR_LOG_COLLECTION" {
								flag, err := strconv.ParseBool(e.Value)
								if err != nil {
									t.Errorf("Invalid ENABLE_VAR_LOG_COLLECTION value: %q", e.Name)
									return HookIncomplete
								}
								if flag {
									t.Errorf("ENABLE_VAR_LOG_COLLECTION = %v, want: %v", flag, false)
									return HookIncomplete
								}
								return HookComplete
							}
						}

						t.Error("ENABLE_VAR_LOG_COLLECTION is not set")
						return HookIncomplete
					}
				}
				t.Logf("The deployment spec doesn't contain the expected container %q", resources.QueueContainerName)
				return HookIncomplete
			}
		},
	}, {
		name: "Update QueueProxy Image", // Should update queueSidecarImage
		configMapToUpdate: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      deployment.ConfigName,
			},
			Data: map[string]string{
				"queueSidecarImage": "myAwesomeQueueImage",
			},
		},
		callback: func(t *testing.T) func(runtime.Object) HookResult {
			return func(obj runtime.Object) HookResult {
				deployment := obj.(*appsv1.Deployment)
				t.Logf("Deployment updated: %v", deployment.Name)

				expected := "myAwesomeQueueImage"

				var got string
				for _, c := range deployment.Spec.Template.Spec.Containers {
					if c.Name == resources.QueueContainerName {
						got = c.Image
						if got == expected {
							return HookComplete
						}
					}
				}

				t.Logf("No update occurred; expected: %s got: %s", expected, got)
				return HookIncomplete
			}
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controllerConfig := getTestDeploymentConfig()
			ctx, informers, ctrl, watcher := newTestControllerWithConfig(t, controllerConfig, nil)

			ctx, cancel := context.WithCancel(ctx)
			grp := errgroup.Group{}

			kubeClient := fakekubeclient.Get(ctx)

			rev := testRevision(getPodSpec())
			revClient := fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace)
			h := NewHooks()
			h.OnUpdate(&kubeClient.Fake, "deployments", test.callback(t))

			// Wait for the deployment creation to trigger the global resync. This
			// avoids the create and update being coalesced into one event.
			h.OnCreate(&kubeClient.Fake, "deployments", func(obj runtime.Object) HookResult {
				watcher.OnChange(test.configMapToUpdate)
				return HookComplete
			})

			waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
			if err != nil {
				t.Fatalf("Failed to start informers: %v", err)
			}
			defer func() {
				cancel()
				if err := grp.Wait(); err != nil {
					t.Errorf("Wait() = %v", err)
				}
				waitInformers()
			}()

			if err := watcher.Start(ctx.Done()); err != nil {
				t.Fatalf("Failed to start configuration manager: %v", err)
			}

			grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

			revClient.Create(rev)

			if err := h.WaitForHooks(3 * time.Second); err != nil {
				t.Errorf("%s Global Resync Failed: %v", test.name, err)
			}
		})
	}
}
