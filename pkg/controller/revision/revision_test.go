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

/* TODO tests:
- When a Revision is updated TODO
- When a Revision is deleted TODO
*/

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/configmap"
	"github.com/knative/serving/pkg/logging"
	. "github.com/knative/serving/pkg/logging/testing"

	"github.com/google/go-cmp/cmp"
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	buildinformers "github.com/knative/build/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	ctrl "github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/revision/config"
	"github.com/knative/serving/pkg/controller/revision/resources"
	resourcenames "github.com/knative/serving/pkg/controller/revision/resources/names"
	appsv1 "k8s.io/api/apps/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakevpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vpainformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"

	corev1 "k8s.io/api/core/v1"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/controller/testing"
)

func getTestConfiguration() *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/configurations/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
	}
}

func getTestReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-service", revName),
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

func getTestNotReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-service", revName),
			Namespace: testNamespace,
			Labels: map[string]string{
				serving.RevisionLabelKey: revName,
			},
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{},
		}},
	}
}

func sumMaps(a map[string]string, b map[string]string) map[string]string {
	summedMap := make(map[string]string, len(a)+len(b)+2)
	for k, v := range a {
		summedMap[k] = v
	}
	for k, v := range b {
		summedMap[k] = v
	}
	return summedMap
}

func newTestControllerWithConfig(t *testing.T, controllerConfig *config.Controller, configs ...*corev1.ConfigMap) (
	kubeClient *fakekubeclientset.Clientset,
	buildClient *fakebuildclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	vpaClient *fakevpaclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	buildInformer buildinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	configMapWatcher configmap.Watcher,
	vpaInformer vpainformers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	buildClient = fakebuildclientset.NewSimpleClientset()
	servingClient = fakeclientset.NewSimpleClientset()
	vpaClient = fakevpaclientset.NewSimpleClientset()

	var cms []*corev1.ConfigMap
	cms = append(cms, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      config.NetworkConfigName,
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      logging.ConfigName,
		},
		Data: map[string]string{
			"zap-logger-config":   "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}",
			"loglevel.queueproxy": "info",
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      config.ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         testFluentdImage,
			"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      autoscaler.ConfigName,
		},
		Data: map[string]string{
			"max-scale-up-rate":           "1.0",
			"single-concurrency-target":   "1.0",
			"multi-concurrency-target":    "1.0",
			"stable-window":               "5m",
			"panic-window":                "10s",
			"scale-to-zero-threshold":     "10m",
			"concurrency-quantum-of-time": "100ms",
		},
	})
	for _, cm := range configs {
		cms = append(cms, cm)
	}

	configMapWatcher = configmap.NewFixedWatcher(cms...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	buildInformer = buildinformers.NewSharedInformerFactory(buildClient, 0)
	servingInformer = informers.NewSharedInformerFactory(servingClient, 0)
	vpaInformer = vpainformers.NewSharedInformerFactory(vpaClient, 0)

	controller = NewController(
		ctrl.Options{
			KubeClientSet:    kubeClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           TestLogger(t),
		},
		vpaClient,
		servingInformer.Serving().V1alpha1().Revisions(),
		buildInformer.Build().V1alpha1().Builds(),
		kubeInformer.Apps().V1().Deployments(),
		kubeInformer.Core().V1().Services(),
		kubeInformer.Core().V1().Endpoints(),
		vpaInformer.Poc().V1alpha1().VerticalPodAutoscalers(),
		controllerConfig,
	)

	controller.resolver = &nopResolver{}

	return
}

func createRevision(t *testing.T,
	kubeClient *fakekubeclientset.Clientset, kubeInformer kubeinformers.SharedInformerFactory,
	servingClient *fakeclientset.Clientset, servingInformer informers.SharedInformerFactory,
	controller *Controller, rev *v1alpha1.Revision) *v1alpha1.Revision {
	t.Helper()
	servingClient.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	if err := controller.Reconcile(KeyOrDie(rev)); err == nil {
		rev, _, _ = addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, rev)
	}
	return rev
}

func updateRevision(t *testing.T,
	kubeClient *fakekubeclientset.Clientset, kubeInformer kubeinformers.SharedInformerFactory,
	servingClient *fakeclientset.Clientset, servingInformer informers.SharedInformerFactory,
	controller *Controller, rev *v1alpha1.Revision) {
	t.Helper()
	servingClient.ServingV1alpha1().Revisions(rev.Namespace).Update(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Update(rev)

	if err := controller.Reconcile(KeyOrDie(rev)); err == nil {
		addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, rev)
	}
}

func makeBackingEndpoints(kubeClient *fakekubeclientset.Clientset,
	kubeInformer kubeinformers.SharedInformerFactory, service *corev1.Service) *corev1.Endpoints {
	endpoints := &corev1.Endpoints{
		ObjectMeta: service.ObjectMeta,
	}
	kubeClient.CoreV1().Endpoints(service.Namespace).Create(endpoints)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(endpoints)
	return endpoints
}

func addResourcesToInformers(t *testing.T,
	kubeClient *fakekubeclientset.Clientset, kubeInformer kubeinformers.SharedInformerFactory,
	servingClient *fakeclientset.Clientset, servingInformer informers.SharedInformerFactory,
	rev *v1alpha1.Revision) (*v1alpha1.Revision, *appsv1.Deployment, *corev1.Service) {
	t.Helper()

	rev, err := servingClient.ServingV1alpha1().Revisions(rev.Namespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Revisions.Get(%v) = %v", rev.Name, err)
	}
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	haveBuild := rev.Spec.BuildName != ""
	inActive := rev.Spec.ServingState != "Active"

	ns := rev.Namespace

	deploymentName := resourcenames.Deployment(rev)
	deployment, err := kubeClient.AppsV1().Deployments(ns).Get(deploymentName, metav1.GetOptions{})
	if apierrs.IsNotFound(err) && (haveBuild || inActive) {
		// If we're doing a Build this won't exist yet.
	} else if err != nil {
		t.Errorf("Deployments.Get(%v) = %v", deploymentName, err)
	} else {
		kubeInformer.Apps().V1().Deployments().Informer().GetIndexer().Add(deployment)
	}

	// Add autoscaler deployment if any
	autoscalerDeployment, err := kubeClient.AppsV1().Deployments(pkg.GetServingSystemNamespace()).Get(resourcenames.Autoscaler(rev), metav1.GetOptions{})
	if err == nil {
		kubeInformer.Apps().V1().Deployments().Informer().GetIndexer().Add(autoscalerDeployment)
	}

	serviceName := resourcenames.K8sService(rev)
	service, err := kubeClient.CoreV1().Services(ns).Get(serviceName, metav1.GetOptions{})
	if apierrs.IsNotFound(err) && (haveBuild || inActive) {
		// If we're doing a Build this won't exist yet.
	} else if err != nil {
		t.Errorf("Services.Get(%v) = %v", serviceName, err)
	} else {
		kubeInformer.Core().V1().Services().Informer().GetIndexer().Add(service)
	}

	// Add autoscaler service if any
	autoscalerService, err := kubeClient.CoreV1().Services(pkg.GetServingSystemNamespace()).Get(resourcenames.Autoscaler(rev), metav1.GetOptions{})
	if err == nil {
		kubeInformer.Core().V1().Services().Informer().GetIndexer().Add(autoscalerService)
	}

	return rev, deployment, service
}

type fixedResolver struct {
	digest string
}

func (r *fixedResolver) Resolve(deploy *appsv1.Deployment) error {
	pod := deploy.Spec.Template.Spec
	for i := range pod.Containers {
		pod.Containers[i].Image = r.digest
	}
	return nil
}

type errorResolver struct {
	error string
}

func (r *errorResolver) Resolve(deploy *appsv1.Deployment) error {
	return errors.New(r.error)
}

func TestResolutionFailed(t *testing.T) {
	kubeClient, _, servingClient, _, controller, kubeInformer, _, servingInformer, _, _ := newTestController(t)

	// Unconditionally return this error during resolution.
	errorMessage := "I am the expected error message, hear me ROAR!"
	controller.resolver = &errorResolver{errorMessage}

	rev := getTestRevision()
	config := getTestConfiguration()
	rev.OwnerReferences = append(rev.OwnerReferences, *ctrl.NewControllerRef(config))

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	rev, err := servingClient.ServingV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Ensure that the Revision status is updated.
	for _, ct := range []v1alpha1.RevisionConditionType{"ContainerHealthy", "Ready"} {
		got := rev.Status.GetCondition(ct)
		want := &v1alpha1.RevisionCondition{
			Type:               ct,
			Status:             corev1.ConditionFalse,
			Reason:             "ContainerMissing",
			Message:            errorMessage,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}
}

// TODO(mattmoor): Add fluentd table testing.
func TestCreateRevUpdateConfigMap_NewData(t *testing.T) {
	kubeClient, _, servingClient, _, controller, kubeInformer, _, servingInformer, _, _ := newTestController(t)
	rev := getTestRevision()

	fluentdConfigSource := makeFullFluentdConfig(testFluentdSidecarOutputConfig)
	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fluentdConfigMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"varlog.conf": "test-config",
		},
	}
	kubeClient.CoreV1().ConfigMaps(testNamespace).Create(existingConfigMap)

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	// Look for the config map.
	configMap, err := kubeClient.CoreV1().ConfigMaps(testNamespace).Get(fluentdConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config map: %v", err)
	}
	if got, want := configMap.Data["varlog.conf"], fluentdConfigSource; got != want {
		t.Errorf("Fluent config file not set correctly config map: expected %v got %v.",
			want, got)
	}
}

// TODO(mattmoor): Add fluentd table testing.
func TestCreateRevUpdateConfigMap_NewRevOwnerReference(t *testing.T) {
	kubeClient, _, servingClient, _, controller, kubeInformer, _, servingInformer, _, _ := newTestController(t)
	rev := getTestRevision()
	revRef := *newRevisionNonControllerRef(rev)
	oldRev := getTestRevision()
	oldRev.Name = "old" + oldRev.Name
	oldRevRef := *newRevisionNonControllerRef(oldRev)

	fluentdConfigSource := makeFullFluentdConfig(testFluentdSidecarOutputConfig)
	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fluentdConfigMapName,
			Namespace:       testNamespace,
			OwnerReferences: []metav1.OwnerReference{oldRevRef},
		},
		Data: map[string]string{
			"varlog.conf": fluentdConfigSource,
		},
	}
	kubeClient.CoreV1().ConfigMaps(testNamespace).Create(existingConfigMap)

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	// Look for the config map.
	configMap, err := kubeClient.CoreV1().ConfigMaps(testNamespace).Get(fluentdConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config map: %v", err)
	}
	expectedRefs := []metav1.OwnerReference{oldRevRef, revRef}
	if diff := cmp.Diff(expectedRefs, configMap.OwnerReferences); diff != "" {
		t.Errorf("Unexpected config map owner refs diff (-want +got): %v", diff)
	}
}

// TODO(mattmoor): Add VPA table testing
func TestCreateRevWithVPA(t *testing.T) {
	controllerConfig := getTestControllerConfig()
	kubeClient, _, servingClient, vpaClient, controller, kubeInformer, _, servingInformer, _, _ := newTestControllerWithConfig(t, controllerConfig, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      autoscaler.ConfigName,
		},
		Data: map[string]string{
			"enable-vertical-pod-autoscaling": "true",
			"max-scale-up-rate":               "1.0",
			"single-concurrency-target":       "1.0",
			"multi-concurrency-target":        "1.0",
			"stable-window":                   "5m",
			"panic-window":                    "10s",
			"scale-to-zero-threshold":         "10m",
			"concurrency-quantum-of-time":     "100ms",
		},
	})
	revClient := servingClient.ServingV1alpha1().Revisions(testNamespace)
	rev := getTestRevision()

	if !controller.getAutoscalerConfig().EnableVPA {
		t.Fatal("EnableVPA = false, want true")
	}

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	createdVPA, err := vpaClient.PocV1alpha1().VerticalPodAutoscalers(testNamespace).Get(resourcenames.VPA(rev), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get vpa: %v", err)
	}
	createdRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Verify label selectors match
	if want, got := createdRev.ObjectMeta.Labels, createdVPA.Spec.Selector.MatchLabels; reflect.DeepEqual(want, got) {
		t.Fatalf("Mismatched labels. Wanted %v. Got %v.", want, got)
	}
}

// TODO(mattmoor): add coverage of a Reconcile fixing a stale logging URL
func TestUpdateRevWithWithUpdatedLoggingURL(t *testing.T) {
	controllerConfig := getTestControllerConfig()
	kubeClient, _, servingClient, _, controller, kubeInformer, _, servingInformer, _, _ := newTestControllerWithConfig(t, controllerConfig, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      config.ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         testFluentdImage,
			"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
			"logging.revision-url-template":         "http://old-logging.test.com?filter=${REVISION_UID}",
		},
	})
	revClient := servingClient.ServingV1alpha1().Revisions(testNamespace)

	rev := getTestRevision()
	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	// Update controllers logging URL
	controller.receiveObservabilityConfig(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      config.ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         testFluentdImage,
			"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
			"logging.revision-url-template":         "http://new-logging.test.com?filter=${REVISION_UID}",
		},
	})
	updateRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	updatedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	expectedLoggingURL := fmt.Sprintf("http://new-logging.test.com?filter=%s", rev.UID)
	if updatedRev.Status.LogURL != expectedLoggingURL {
		t.Errorf("Updated revision does not have an updated logging URL: expected: %s, got: %s", expectedLoggingURL, updatedRev.Status.LogURL)
	}
}

// TODO(mattmoor): Remove when we have coverage of EnqueueBuildTrackers
func TestCreateRevWithCompletedBuildNameCompletes(t *testing.T) {
	kubeClient, buildClient, servingClient, _, controller, kubeInformer, buildInformer, servingInformer, _, _ := newTestController(t)

	h := NewHooks()
	// Look for the build complete event. Events are delivered asynchronously so
	// we need to use hooks here.
	h.OnCreate(&kubeClient.Fake, "events", func(obj runtime.Object) HookResult {
		event := obj.(*corev1.Event)
		if wanted, got := "BuildSucceeded", event.Reason; wanted != got {
			t.Errorf("unexpected event reason: %q expected: %q", got, wanted)
		}
		if wanted, got := corev1.EventTypeNormal, event.Type; wanted != got {
			t.Errorf("unexpected event Type: %q expected: %q", got, wanted)
		}
		return HookComplete
	})

	completeMessage := "a long human-readable complete message."

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "foo",
		},
		Spec: buildv1alpha1.BuildSpec{
			Steps: []corev1.Container{{
				Name:    "nop",
				Image:   "busybox:latest",
				Command: []string{"/bin/sh"},
				Args:    []string{"-c", "echo Hello"},
			}},
		},
	}
	buildClient.BuildV1alpha1().Builds(testNamespace).Create(bld)
	// Since Reconcile looks in the lister, we need to add it to the informer
	buildInformer.Build().V1alpha1().Builds().Informer().GetIndexer().Add(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name

	rev = createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	// After the initial update to the revision, we should be
	// watching for this build to complete, so make it complete
	// successfully.
	bld.Status = buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{{
			Type:    buildv1alpha1.BuildSucceeded,
			Status:  corev1.ConditionTrue,
			Message: completeMessage,
		}},
	}
	// Since Reconcile looks in the lister, we need to add it to the informer
	buildInformer.Build().V1alpha1().Builds().Informer().GetIndexer().Add(bld)

	controller.EnqueueBuildTrackers(bld)
	controller.Reconcile(KeyOrDie(rev))

	// Make sure that the changes from the Reconcile are reflected in our Informers.
	completedRev, _, _ := addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, rev)

	// The next update we receive should tell us that the build completed.
	for _, ct := range []v1alpha1.RevisionConditionType{"BuildSucceeded"} {
		got := completedRev.Status.GetCondition(ct)
		want := &v1alpha1.RevisionCondition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			Message:            completeMessage,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

// TODO(mattmoor): Remove when we have coverage of EnqueueEndpointsRevision
func TestMarkRevReadyUponEndpointBecomesReady(t *testing.T) {
	kubeClient, _, servingClient, _, controller, kubeInformer, _, servingInformer, _, _ := newTestController(t)
	rev := getTestRevision()

	h := NewHooks()
	// Look for the revision ready event. Events are delivered asynchronously so
	// we need to use hooks here.
	expectedMessage := "Revision becomes ready upon endpoint \"test-rev-service\" becoming ready"
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, expectedMessage))

	deployingRev := createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	// The revision is not marked ready until an endpoint is created.
	for _, ct := range []v1alpha1.RevisionConditionType{"Ready"} {
		got := deployingRev.Status.GetCondition(ct)
		want := &v1alpha1.RevisionCondition{
			Type:               ct,
			Status:             corev1.ConditionUnknown,
			Reason:             "Deploying",
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}

	endpoints := getTestReadyEndpoints(rev.Name)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(endpoints)
	controller.EnqueueEndpointsRevision(endpoints)
	controller.Reconcile(KeyOrDie(rev))

	// Make sure that the changes from the Reconcile are reflected in our Informers.
	readyRev, _, _ := addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, rev)

	// After reconciling the endpoint, the revision should be ready.
	for _, ct := range []v1alpha1.RevisionConditionType{"Ready"} {
		got := readyRev.Status.GetCondition(ct)
		want := &v1alpha1.RevisionCondition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

// TODO(mattmoor): Remove once we have table testing with the autoscaler configured so that it doesn't use single-tenant autoscaling.
func TestNoAutoscalerImageCreatesNoAutoscalers(t *testing.T) {
	controllerConfig := getTestControllerConfig()
	controllerConfig.AutoscalerImage = ""
	kubeClient, _, servingClient, _, controller, kubeInformer, _, servingInformer, _, _ := newTestControllerWithConfig(t, controllerConfig)

	rev := getTestRevision()
	config := getTestConfiguration()
	rev.OwnerReferences = append(
		rev.OwnerReferences,
		*ctrl.NewControllerRef(config),
	)

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	expectedAutoscalerName := fmt.Sprintf("%s-autoscaler", rev.Name)

	// Look for the autoscaler deployment.
	_, err := kubeClient.AppsV1().Deployments(pkg.GetServingSystemNamespace()).Get(expectedAutoscalerName, metav1.GetOptions{})
	if !apierrs.IsNotFound(err) {
		t.Errorf("Expected autoscaler deployment %s to not exist.", expectedAutoscalerName)
	}

	// Look for the autoscaler service.
	_, err = kubeClient.CoreV1().Services(pkg.GetServingSystemNamespace()).Get(expectedAutoscalerName, metav1.GetOptions{})
	if !apierrs.IsNotFound(err) {
		t.Errorf("Expected autoscaler service %s to not exist.", expectedAutoscalerName)
	}
}

// This covers *error* paths in receiveNetworkConfig, since "" is not a valid value.
func TestIstioOutboundIPRangesInjection(t *testing.T) {
	var annotations map[string]string

	// A valid IP range
	want := "10.10.10.0/24"
	annotations = getPodAnnotationsForConfig(t, want, "")
	if got := annotations[resources.IstioOutboundIPRangeAnnotation]; want != got {
		t.Fatalf("%v annotation expected to be %v, but is %v.", resources.IstioOutboundIPRangeAnnotation, want, got)
	}

	// An invalid IP range
	want = "10.10.10.10/33"
	annotations = getPodAnnotationsForConfig(t, want, "")
	if got, ok := annotations[resources.IstioOutboundIPRangeAnnotation]; ok {
		t.Fatalf("Expected to have no %v annotation for invalid option %v. But found value %v", resources.IstioOutboundIPRangeAnnotation, want, got)
	}

	// Configuration has an annotation override - its value must be preserved
	want = "10.240.10.0/14"
	annotations = getPodAnnotationsForConfig(t, "", want)
	if got := annotations[resources.IstioOutboundIPRangeAnnotation]; got != want {
		t.Fatalf("%v annotation is expected to have %v but got %v", resources.IstioOutboundIPRangeAnnotation, want, got)
	}
	annotations = getPodAnnotationsForConfig(t, "10.10.10.0/24", want)
	if got := annotations[resources.IstioOutboundIPRangeAnnotation]; got != want {
		t.Fatalf("%v annotation is expected to have %v but got %v", resources.IstioOutboundIPRangeAnnotation, want, got)
	}
}

// TODO(mattmoor): Add table testing that varies the replica counts of Deployments.
func TestReconcileReplicaCount(t *testing.T) {
	kubeClient, _, servingClient, _, controller, kubeInformer, _, elaInformer, _, _ := newTestController(t)
	rev := getTestRevision()

	rev.Spec.ServingState = v1alpha1.RevisionServingStateReserve
	createRevision(t, kubeClient, kubeInformer, servingClient, elaInformer, controller, rev)
	getDeployments := func() (*appsv1.Deployment, *appsv1.Deployment) {
		d1, err := kubeClient.AppsV1().Deployments(testNamespace).Get(resourcenames.Deployment(rev), metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Expected to have a deployment but found none: %v", err)
		}
		d2, err := kubeClient.AppsV1().Deployments(pkg.GetServingSystemNamespace()).Get(resourcenames.Autoscaler(rev), metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Expected to have an autoscaler deployment but found none: %v", err)
		}
		return d1, d2
	}

	d1, d2 := getDeployments()

	// Update the replica count to a positive number. This should get reconciled back to 0.
	d1.Spec.Replicas = new(int32)
	*d1.Spec.Replicas = 10
	d2.Spec.Replicas = new(int32)
	*d2.Spec.Replicas = 20
	kubeClient.AppsV1().Deployments(testNamespace).Update(d1)
	kubeClient.AppsV1().Deployments(pkg.GetServingSystemNamespace()).Update(d2)
	kubeInformer.Apps().V1().Deployments().Informer().GetIndexer().Update(d1)
	kubeInformer.Apps().V1().Deployments().Informer().GetIndexer().Update(d2)
	updateRevision(t, kubeClient, kubeInformer, servingClient, elaInformer, controller, rev)
	d1, d2 = getDeployments()
	if *d1.Spec.Replicas != 0 {
		t.Fatalf("Expected deployment to have 0 replicas, got: %v", *d1.Spec.Replicas)
	}
	if *d2.Spec.Replicas != 0 {
		t.Fatalf("Expected autoscaler deployment to have 0 replicas, got: %v", *d2.Spec.Replicas)
	}

	// Activate the revision. Replicas should increase to 1
	rev.Spec.ServingState = v1alpha1.RevisionServingStateActive
	updateRevision(t, kubeClient, kubeInformer, servingClient, elaInformer, controller, rev)
	d1, d2 = getDeployments()
	if *d1.Spec.Replicas != 1 {
		t.Fatalf("Expected deployment to have 1 replicas, got: %v", *d1.Spec.Replicas)
	}
	if *d2.Spec.Replicas != 1 {
		t.Fatalf("Expected autoscaler deployment to have 1 replicas, got: %v", *d2.Spec.Replicas)
	}

	// Increase the replica count - those should be kept intact
	d1.Spec.Replicas = new(int32)
	*d1.Spec.Replicas = 30
	d2.Spec.Replicas = new(int32)
	*d2.Spec.Replicas = 40
	kubeClient.AppsV1().Deployments(testNamespace).Update(d1)
	kubeClient.AppsV1().Deployments(pkg.GetServingSystemNamespace()).Update(d2)
	kubeInformer.Apps().V1().Deployments().Informer().GetIndexer().Update(d1)
	kubeInformer.Apps().V1().Deployments().Informer().GetIndexer().Update(d2)
	updateRevision(t, kubeClient, kubeInformer, servingClient, elaInformer, controller, rev)
	d1, d2 = getDeployments()
	if *d1.Spec.Replicas != 30 {
		t.Fatalf("Expected deployment to have 30 replicas, got: %v", *d1.Spec.Replicas)
	}
	if *d2.Spec.Replicas != 40 {
		t.Fatalf("Expected autoscaler deployment to have 40 replicas, got: %v", *d2.Spec.Replicas)
	}

	// Deactivate the revision. Replicas should go back to 0.
	rev.Spec.ServingState = v1alpha1.RevisionServingStateReserve
	updateRevision(t, kubeClient, kubeInformer, servingClient, elaInformer, controller, rev)
	d1, d2 = getDeployments()
	if *d1.Spec.Replicas != 0 {
		t.Fatalf("Expected deployment to have 0 replicas, got: %v", *d1.Spec.Replicas)
	}
	if *d2.Spec.Replicas != 0 {
		t.Fatalf("Expected autoscaler deployment to have 0 replicas, got: %v", *d2.Spec.Replicas)
	}
}

func getPodAnnotationsForConfig(t *testing.T, configMapValue string, configAnnotationOverride string) map[string]string {
	controllerConfig := getTestControllerConfig()
	kubeClient, _, servingClient, _, controller, kubeInformer, _, servingInformer, _, _ := newTestControllerWithConfig(t, controllerConfig)

	// Resolve image references to this "digest"
	digest := "foo@sha256:deadbeef"
	controller.resolver = &fixedResolver{digest}
	controller.receiveNetworkConfig(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.NetworkConfigName,
			Namespace: pkg.GetServingSystemNamespace(),
		},
		Data: map[string]string{
			config.IstioOutboundIPRangesKey: configMapValue,
		}})

	rev := getTestRevision()
	config := getTestConfiguration()
	if len(configAnnotationOverride) > 0 {
		rev.ObjectMeta.Annotations = map[string]string{resources.IstioOutboundIPRangeAnnotation: configAnnotationOverride}
	}

	rev.OwnerReferences = append(
		rev.OwnerReferences,
		*ctrl.NewControllerRef(config),
	)

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, controller, rev)

	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get serving deployment: %v", err)
	}
	return deployment.Spec.Template.ObjectMeta.Annotations
}
