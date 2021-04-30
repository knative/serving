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

package revision

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	// Inject the fakes for informers this controller relies on.
	fakecachingclient "knative.dev/caching/pkg/client/injection/client/fake"
	fakeimageinformer "knative.dev/caching/pkg/client/injection/informers/caching/v1alpha1/image/fake"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakedeploymentinformer "knative.dev/pkg/client/injection/kube/informers/apps/v1/deployment/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/configmap/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/ptr"
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	tracingconfig "knative.dev/pkg/tracing/config"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/reconciler/revision/resources"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"

	_ "knative.dev/pkg/metrics/testing"
	. "knative.dev/pkg/reconciler/testing"
)

const (
	testAutoscalerImage = "autoscalerImage"
	testNamespace       = "test"
	testQueueImage      = "queueImage"
)

func newTestController(t *testing.T, configs []*corev1.ConfigMap, opts ...reconcilerOption) (
	context.Context,
	context.CancelFunc,
	[]controller.Informer,
	*controller.Impl,
	*configmap.ManualWatcher) {

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	t.Cleanup(cancel) // cancel is reentrant, so if necessary callers can call it directly, if needed.
	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}

	// Prepend so that callers can override.
	opts = append([]reconcilerOption{func(r *Reconciler) {
		r.resolver = &nopResolver{}
	}}, opts...)
	controller := newControllerWithOptions(ctx, configMapWatcher, opts...)

	for _, cm := range append([]*corev1.ConfigMap{{
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
			Name:      config.FeaturesConfigName,
		},
		Data: map[string]string{},
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
			"zipkin-endpoint": "http://zipkin.istio-system.svc:9411/api/v2/spans",
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
	}, testDeploymentCM(), testDefaultsCM()},
		configs...) {
		configMapWatcher.OnChange(cm)
	}

	// The Reconciler won't do any work until it becomes the leader.
	if la, ok := controller.Reconciler.(reconciler.LeaderAware); ok {
		la.Promote(reconciler.UniversalBucket(), func(reconciler.Bucket, types.NamespacedName) {})
	}

	return ctx, cancel, informers, controller, configMapWatcher
}

func createRevision(
	t *testing.T,
	ctx context.Context,
	controller *controller.Impl,
	rev *v1.Revision,
) *v1.Revision {
	t.Helper()
	fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
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
	fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Update(ctx, rev, metav1.UpdateOptions{})
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Update(rev)

	if err := controller.Reconciler.Reconcile(context.Background(), KeyOrDie(rev)); err == nil {
		addResourcesToInformers(t, ctx, rev)
	}
}

func addResourcesToInformers(t *testing.T, ctx context.Context, rev *v1.Revision) (*v1.Revision, *appsv1.Deployment, *autoscalingv1alpha1.PodAutoscaler) {
	t.Helper()

	rev, err := fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Get(ctx, rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Revisions.Get(%v) = %v", rev.Name, err)
	}
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	ns := rev.Namespace

	paName := names.PA(rev)
	pa, err := fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(rev.Namespace).Get(ctx, paName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("PodAutoscalers.Get(%v) = %v", paName, err)
	}
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(pa)

	for _, v := range rev.Spec.Containers {
		imageName := kmeta.ChildName(names.ImageCache(rev), "-"+v.Name)
		image, err := fakecachingclient.Get(ctx).CachingV1alpha1().Images(rev.Namespace).Get(ctx, imageName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Caching.Images.Get(%v) = %v", imageName, err)
		}
		fakeimageinformer.Get(ctx).Informer().GetIndexer().Add(image)
	}

	deploymentName := names.Deployment(rev)
	deployment, err := fakekubeclient.Get(ctx).AppsV1().Deployments(ns).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Deployments.Get(%v) = %v", deploymentName, err)
	}
	fakedeploymentinformer.Get(ctx).Informer().GetIndexer().Add(deployment)
	return rev, deployment, pa
}

type nopResolver struct{}

func (r *nopResolver) Resolve(rev *v1.Revision, _ k8schain.Options, _ sets.String, _ time.Duration) ([]v1.ContainerStatus, error) {
	return []v1.ContainerStatus{{
		Name: rev.Spec.Containers[0].Name,
	}}, nil
}

func (r *nopResolver) Clear(types.NamespacedName)  {}
func (r *nopResolver) Forget(types.NamespacedName) {}

func testPodSpec() corev1.PodSpec {
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
			Generation: rand.Int63(),
		},
		Spec: v1.RevisionSpec{
			PodSpec:        podSpec,
			TimeoutSeconds: ptr.Int64(60),
		},
	}
	rev.SetDefaults(context.Background())
	return rev
}

func testDeploymentConfig() *deployment.Config {
	c, _ := deployment.NewConfigFromConfigMap(testDeploymentCM())
	// ignoring error as test controller is generated
	return c
}

func testDeploymentCM() *corev1.ConfigMap {
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

func testDefaultsCM() *corev1.ConfigMap {
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

type notResolvedYetResolver struct{}

func (r *notResolvedYetResolver) Resolve(_ *v1.Revision, _ k8schain.Options, _ sets.String, _ time.Duration) ([]v1.ContainerStatus, error) {
	return nil, nil
}

func (r *notResolvedYetResolver) Clear(types.NamespacedName)  {}
func (r *notResolvedYetResolver) Forget(types.NamespacedName) {}

type errorResolver struct {
	err     error
	cleared bool
}

func (r *errorResolver) Resolve(_ *v1.Revision, _ k8schain.Options, _ sets.String, _ time.Duration) ([]v1.ContainerStatus, error) {
	return nil, r.err
}

func (r *errorResolver) Clear(types.NamespacedName) {
	r.cleared = true
}

func (r *errorResolver) Forget(types.NamespacedName) {}

func TestResolutionFailed(t *testing.T) {
	// Unconditionally return this error during resolution.
	innerError := errors.New("i am the expected error message, hear me ROAR")
	resolver := &errorResolver{cleared: false, err: innerError}
	ctx, _, _, controller, _ := newTestController(t, nil /*additional CMs*/, func(r *Reconciler) {
		r.resolver = resolver
	})

	rev := testRevision(testPodSpec())
	createRevision(t, ctx, controller, rev)

	rev, err := fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Get(ctx, rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Couldn't get revision:", err)
	}

	// Ensure that the Revision status is updated.
	for _, ct := range []apis.ConditionType{"ContainerHealthy", "Ready"} {
		got := rev.Status.GetCondition(ct)
		want := &apis.Condition{
			Type:               ct,
			Status:             corev1.ConditionFalse,
			Reason:             "ContainerMissing",
			Message:            innerError.Error(),
			LastTransitionTime: got.LastTransitionTime,
			Severity:           apis.ConditionSeverityError,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got):\n%s", diff)
		}
	}

	if !resolver.cleared {
		t.Fatal("Expected resolver.Clear() to have been called")
	}
}

func TestUpdateRevWithWithUpdatedLoggingURL(t *testing.T) {
	ctx, _, _, controller, watcher := newTestController(t, []*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      metrics.ConfigMapName(),
		},
		Data: map[string]string{
			"logging.enable-var-log-collection": "true",
			"logging.revision-url-template":     "http://old-logging.test.com?filter=${REVISION_UID}",
		},
	}, testDeploymentCM(),
	})
	revClient := fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace)

	rev := testRevision(testPodSpec())
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

	updatedRev, err := revClient.Get(ctx, rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Couldn't get revision:", err)
	}

	expectedLoggingURL := "http://new-logging.test.com?filter=" + string(rev.UID)
	if updatedRev.Status.LogURL != expectedLoggingURL {
		t.Errorf("Updated revision does not have an updated logging URL: expected: %s, got: %s", expectedLoggingURL, updatedRev.Status.LogURL)
	}
}

func TestStatusUnknownWhenDigestsNotResolvedYet(t *testing.T) {
	ctx, _, _, controller, _ := newTestController(t, nil /*additional CMs*/, func(r *Reconciler) {
		r.resolver = &notResolvedYetResolver{}
	})

	rev := testRevision(testPodSpec())

	fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)
	if err := controller.Reconciler.Reconcile(context.Background(), KeyOrDie(rev)); err != nil {
		t.Fatal("Reconcile failed:", err)
	}

	rev, err := fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace).Get(ctx, rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Couldn't get revision:", err)
	}

	// Status should be Unknown until the digest resolution completes.
	for _, ct := range []apis.ConditionType{"ResourcesAvailable", "Ready"} {
		got := rev.Status.GetCondition(ct)
		want := &apis.Condition{
			Type:               ct,
			Status:             corev1.ConditionUnknown,
			Reason:             "ResolvingDigests",
			Message:            "",
			LastTransitionTime: got.LastTransitionTime,
			Severity:           apis.ConditionSeverityError,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff for condition %q (-want +got):\n%s", ct, diff)
		}
	}
}

func TestGlobalResyncOnDefaultCMChange(t *testing.T) {
	ctx, cancel, informers, ctrl, watcher := newTestController(t, nil /*additional CMs*/)

	grp := errgroup.Group{}

	rev := testRevision(testPodSpec())
	revClient := fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace)

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Error("Wait() = ", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatal("Failed to start watcher:", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	revClient.Create(ctx, rev, metav1.CreateOptions{})
	revL := fakerevisioninformer.Get(ctx).Lister()
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		// The only error we're getting in the test reasonably is NotFound.
		r, _ := revL.Revisions(rev.Namespace).Get(rev.Name)
		return r != nil && r.Status.ObservedGeneration == r.Generation, nil
	}); err != nil {
		t.Fatal("Failed to see Revision reconciliation:", err)
	}
	t.Log("Saw revision reconciliation")

	// Ensure initial PA is in the informers.
	paL := fakepainformer.Get(ctx).Lister().PodAutoscalers(rev.Namespace)
	if ierr := wait.PollImmediate(50*time.Millisecond, 6*time.Second, func() (bool, error) {
		_, err = paL.Get(rev.Name)
		if apierrs.IsNotFound(err) {
			return false, err
		}
		return err == nil, err
	}); ierr != nil {
		t.Fatal("Failed to see PA creation:", ierr)
	}

	// The code in the loop is racy. So we execute it a few times.
	enough := time.After(time.Minute)
	pos := int64(41)
	for ; ; pos++ {
		select {
		case <-enough:
			t.Fatal("No iteration succeeded to see the global resync")
		default:
		}
		// Re-get it and nillify the CC, to ensure defaulting
		// happens as expected.
		rev, _ = revL.Revisions(rev.Namespace).Get(rev.Name)
		rev = rev.DeepCopy()
		rev.Spec.ContainerConcurrency = nil
		rev.Generation++
		revClient.Update(ctx, rev, metav1.UpdateOptions{})

		watcher.OnChange(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      config.DefaultsConfigName,
			},
			Data: map[string]string{
				"container-concurrency": fmt.Sprint(pos),
			},
		})

		pa, err := paL.Get(rev.Name)
		t.Logf("Initial PA: %#v GetErr: %v", pa, err)
		if ierr := wait.PollImmediate(50*time.Millisecond, 2*time.Second, func() (bool, error) {
			pa, err = paL.Get(rev.Name)
			return pa != nil && pa.Spec.ContainerConcurrency == pos, err
		}); ierr == nil { // err==nil!
			break
		}
	}
}

func TestGlobalResyncOnConfigMapUpdateRevision(t *testing.T) {
	ctx, cancel, informers, ctrl, watcher := newTestController(t, nil /*additional CMs*/)

	grp := errgroup.Group{}

	rev := testRevision(testPodSpec())
	revClient := fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace)

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Error("Wait() = ", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatal("Failed to start watcher:", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	revClient.Create(ctx, rev, metav1.CreateOptions{})
	revL := fakerevisioninformer.Get(ctx).Lister()
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		// The only error we're getting in the test reasonably is NotFound.
		r, _ := revL.Revisions(rev.Namespace).Get(rev.Name)
		// We only create a single revision, but make sure it is reconciled.
		return r != nil && r.Status.ObservedGeneration == r.Generation, nil
	}); err != nil {
		t.Fatal("Failed to see Revision propagation:", err)
	}
	t.Log("Seen revision propagation")

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

	want := "http://new-logging.test.com?filter=" + string(rev.UID)
	if ierr := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (bool, error) {
		r, err := revL.Revisions(rev.Namespace).Get(rev.Name)
		return r != nil && r.Status.LogURL == want, err
	}); ierr != nil {
		t.Fatal("Failed to see Revision propagation:", ierr)
	}
}

func TestGlobalResyncOnConfigMapUpdateDeployment(t *testing.T) {
	// Test that changes to the ConfigMap result in the desired changes on an existing
	// deployment.
	configMapToUpdate := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      deployment.ConfigName,
		},
		Data: map[string]string{
			"queueSidecarImage": "myAwesomeQueueImage",
		},
	}
	const expected = "myAwesomeQueueImage"
	checkF := func(deployment *appsv1.Deployment) bool {
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == resources.QueueContainerName {
				return c.Image == expected
			}
		}
		return false
	}

	ctx, cancel, informers, ctrl, watcher := newTestController(t, nil /*additional CMs*/)

	grp := errgroup.Group{}
	rev := testRevision(testPodSpec())
	revClient := fakeservingclient.Get(ctx).ServingV1().Revisions(rev.Namespace)

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Error("Wait() = ", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatal("Failed to start configuration manager:", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	revClient.Create(ctx, rev, metav1.CreateOptions{})
	revL := fakerevisioninformer.Get(ctx).Lister().Revisions(rev.Namespace)
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		// The only error we're getting in the test reasonably is NotFound.
		r, _ := revL.Get(rev.Name)
		// We only create a single revision, but make sure it is reconciled.
		return r != nil && r.Status.ObservedGeneration == r.Generation, nil
	}); err != nil {
		t.Fatal("Failed to see Revision propagation:", err)
	}
	t.Log("Seen revision propagation updating the CM")

	watcher.OnChange(configMapToUpdate)

	depL := fakedeploymentinformer.Get(ctx).Lister().Deployments(rev.Namespace)
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		dep, err := depL.Get(names.Deployment(rev))
		return dep != nil && checkF(dep), err
	}); err != nil {
		t.Error("Failed to see deployment properly updating:", err)
	}
}

func TestNewRevisionCallsSyncHandler(t *testing.T) {
	ctx, cancel, informers, ctrl, _ := newTestController(t, nil /*additional CMs*/)

	eg := errgroup.Group{}
	rev := testRevision(testPodSpec())
	servingClient := fakeservingclient.Get(ctx)

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
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
		return ctrl.Run(1, ctx.Done())
	})

	if _, err := servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating revision:", err)
	}

	// Poll to see PA object to be created.
	if err := wait.PollImmediate(25*time.Millisecond, 3*time.Second, func() (bool, error) {
		pa, _ := servingClient.AutoscalingV1alpha1().PodAutoscalers(rev.Namespace).Get(
			ctx, rev.Name, metav1.GetOptions{})
		return pa != nil, nil
	}); err != nil {
		t.Error("Failed to see PA creation")
	}

	// Poll to see if the deployment is created. This should _already_ be there.
	depL := fakedeploymentinformer.Get(ctx).Lister().Deployments(rev.Namespace)
	if err := wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		dep, err := depL.Get(names.Deployment(rev))
		return dep != nil, err
	}); err != nil {
		t.Error("Failed to see deployment creation:", err)
	}
}
