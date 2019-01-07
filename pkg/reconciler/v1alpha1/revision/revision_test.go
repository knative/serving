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
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	fakecachingclientset "github.com/knative/caching/pkg/client/clientset/versioned/fake"
	cachinginformers "github.com/knative/caching/pkg/client/informers/externalversions"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/configmap"
	ctrl "github.com/knative/pkg/controller"
	"github.com/knative/pkg/kmeta"
	kpav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/logging"
	rclr "github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"github.com/knative/serving/pkg/system"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
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

func getTestReadyKPA(rev *v1alpha1.Revision) *kpav1alpha1.PodAutoscaler {
	kpa := resources.MakeKPA(rev)
	kpa.Status.InitializeConditions()
	kpa.Status.MarkActive()
	return kpa
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
	servingClient *fakeclientset.Clientset,
	cachingClient *fakecachingclientset.Clientset,
	dynamicClient *fakedynamic.FakeDynamicClient,
	controller *ctrl.Impl,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	cachingInformer cachinginformers.SharedInformerFactory,
	configMapWatcher *configmap.ManualWatcher,
	buildInformerFactory duck.InformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	servingClient = fakeclientset.NewSimpleClientset()
	cachingClient = fakecachingclientset.NewSimpleClientset()
	dynamicClient = fakedynamic.NewSimpleDynamicClient(runtime.NewScheme())

	configMapWatcher = &configmap.ManualWatcher{Namespace: system.Namespace()}

	opt := rclr.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		DynamicClientSet: dynamicClient,
		CachingClientSet: cachingClient,
		ConfigMapWatcher: configMapWatcher,
		Logger:           TestLogger(t),
		ResyncPeriod:     0,
		StopChannel:      nil,
	}

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, opt.ResyncPeriod)
	servingInformer = informers.NewSharedInformerFactory(servingClient, opt.ResyncPeriod)
	cachingInformer = cachinginformers.NewSharedInformerFactory(cachingClient, opt.ResyncPeriod)
	buildInformerFactory = KResourceTypedInformerFactory(opt)

	controller = NewController(
		opt,
		servingInformer.Serving().V1alpha1().Revisions(),
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		cachingInformer.Caching().V1alpha1().Images(),
		kubeInformer.Apps().V1().Deployments(),
		kubeInformer.Core().V1().Services(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Core().V1().ConfigMaps(),
		buildInformerFactory,
	)

	controller.Reconciler.(*Reconciler).resolver = &nopResolver{}

	var cms []*corev1.ConfigMap
	cms = append(cms, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      config.NetworkConfigName,
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      logging.ConfigName,
		},
		Data: map[string]string{
			"zap-logger-config":   "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}",
			"loglevel.queueproxy": "info",
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      config.ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         testFluentdImage,
			"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      autoscaler.ConfigName,
		},
		Data: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
		},
	},
		getTestControllerConfigMap(),
	)

	cms = append(cms, configs...)

	for _, configMap := range cms {
		configMapWatcher.OnChange(configMap)
	}

	return
}

func createRevision(
	t *testing.T,
	kubeClient *fakekubeclientset.Clientset,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingClient *fakeclientset.Clientset,
	servingInformer informers.SharedInformerFactory,
	cachingClient *fakecachingclientset.Clientset,
	cachingInformer cachinginformers.SharedInformerFactory,
	controller *ctrl.Impl,
	rev *v1alpha1.Revision,
) *v1alpha1.Revision {
	t.Helper()
	servingClient.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	if err := controller.Reconciler.Reconcile(context.TODO(), KeyOrDie(rev)); err == nil {
		rev, _, _ = addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, rev)
	}
	return rev
}

func updateRevision(
	t *testing.T,
	kubeClient *fakekubeclientset.Clientset,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingClient *fakeclientset.Clientset,
	servingInformer informers.SharedInformerFactory,
	cachingClient *fakecachingclientset.Clientset,
	cachingInformer cachinginformers.SharedInformerFactory,
	controller *ctrl.Impl,
	rev *v1alpha1.Revision,
) {

	t.Helper()
	servingClient.ServingV1alpha1().Revisions(rev.Namespace).Update(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Update(rev)

	if err := controller.Reconciler.Reconcile(context.TODO(), KeyOrDie(rev)); err == nil {
		addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, rev)
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
	cachingClient *fakecachingclientset.Clientset, cachingInformer cachinginformers.SharedInformerFactory,
	rev *v1alpha1.Revision) (*v1alpha1.Revision, *appsv1.Deployment, *corev1.Service) {
	t.Helper()

	rev, err := servingClient.ServingV1alpha1().Revisions(rev.Namespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Revisions.Get(%v) = %v", rev.Name, err)
	}
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	haveBuild := rev.Spec.BuildRef != nil

	ns := rev.Namespace

	kpaName := resourcenames.KPA(rev)
	kpa, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(rev.Namespace).Get(kpaName, metav1.GetOptions{})
	if apierrs.IsNotFound(err) && haveBuild {
		// If we're doing a Build this won't exist yet.
	} else if err != nil {
		t.Errorf("PodAutoscalers.Get(%v) = %v", kpaName, err)
	} else {
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)
	}

	imageName := resourcenames.ImageCache(rev)
	image, err := cachingClient.CachingV1alpha1().Images(rev.Namespace).Get(imageName, metav1.GetOptions{})
	if apierrs.IsNotFound(err) && haveBuild {
		// If we're doing a Build this won't exist yet.
	} else if err != nil {
		t.Errorf("Caching.Images.Get(%v) = %v", imageName, err)
	} else {
		cachingInformer.Caching().V1alpha1().Images().Informer().GetIndexer().Add(image)
	}

	deploymentName := resourcenames.Deployment(rev)
	deployment, err := kubeClient.AppsV1().Deployments(ns).Get(deploymentName, metav1.GetOptions{})
	if apierrs.IsNotFound(err) && haveBuild {
		// If we're doing a Build this won't exist yet.
	} else if err != nil {
		t.Errorf("Deployments.Get(%v) = %v", deploymentName, err)
	} else {
		kubeInformer.Apps().V1().Deployments().Informer().GetIndexer().Add(deployment)
	}

	serviceName := resourcenames.K8sService(rev)
	service, err := kubeClient.CoreV1().Services(ns).Get(serviceName, metav1.GetOptions{})
	if apierrs.IsNotFound(err) && haveBuild {
		// If we're doing a Build this won't exist yet.
	} else if err != nil {
		t.Errorf("Services.Get(%v) = %v", serviceName, err)
	} else {
		kubeInformer.Core().V1().Services().Informer().GetIndexer().Add(service)
	}

	// Add fluentd configmap if any
	fluentdConfigMap, err := kubeClient.CoreV1().ConfigMaps(rev.Namespace).Get(resourcenames.FluentdConfigMap(rev), metav1.GetOptions{})
	if err == nil {
		kubeInformer.Core().V1().ConfigMaps().Informer().GetIndexer().Add(fluentdConfigMap)
	}

	return rev, deployment, service
}

type fixedResolver struct {
	digest string
}

func (r *fixedResolver) Resolve(_ string, _ k8schain.Options, _ map[string]struct{}) (string, error) {
	return r.digest, nil
}

type errorResolver struct {
	error string
}

func (r *errorResolver) Resolve(_ string, _ k8schain.Options, _ map[string]struct{}) (string, error) {
	return "", errors.New(r.error)
}

func TestResolutionFailed(t *testing.T) {
	kubeClient, servingClient, cachingClient, _, controller, kubeInformer, servingInformer, cachingInformer, _, _ := newTestController(t, nil)

	// Unconditionally return this error during resolution.
	errorMessage := "I am the expected error message, hear me ROAR!"
	controller.Reconciler.(*Reconciler).resolver = &errorResolver{errorMessage}

	rev := getTestRevision()
	config := getTestConfiguration()
	rev.OwnerReferences = append(rev.OwnerReferences, *kmeta.NewControllerRef(config))

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, controller, rev)

	rev, err := servingClient.ServingV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Ensure that the Revision status is updated.
	for _, ct := range []duckv1alpha1.ConditionType{"ContainerHealthy", "Ready"} {
		got := rev.Status.GetCondition(ct)
		want := &duckv1alpha1.Condition{
			Type:               ct,
			Status:             corev1.ConditionFalse,
			Reason:             "ContainerMissing",
			Message:            v1alpha1.RevisionContainerMissingMessage(rev.Spec.Container.Image, errorMessage),
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}
}

// TODO(mattmoor): add coverage of a Reconcile fixing a stale logging URL
func TestUpdateRevWithWithUpdatedLoggingURL(t *testing.T) {
	controllerConfig := getTestControllerConfig()
	kubeClient, servingClient, cachingClient, _, controller, kubeInformer, servingInformer, cachingInformer, watcher, _ := newTestControllerWithConfig(t, controllerConfig, &corev1.ConfigMap{

		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      config.ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         testFluentdImage,
			"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
			"logging.revision-url-template":         "http://old-logging.test.com?filter=${REVISION_UID}",
		},
	}, getTestControllerConfigMap(),
	)
	revClient := servingClient.ServingV1alpha1().Revisions(testNamespace)

	rev := getTestRevision()
	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, controller, rev)

	// Update controllers logging URL
	watcher.OnChange(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      config.ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         testFluentdImage,
			"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
			"logging.revision-url-template":         "http://new-logging.test.com?filter=${REVISION_UID}",
		},
	})
	updateRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, controller, rev)

	updatedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	expectedLoggingURL := fmt.Sprintf("http://new-logging.test.com?filter=%s", rev.UID)
	if updatedRev.Status.LogURL != expectedLoggingURL {
		t.Errorf("Updated revision does not have an updated logging URL: expected: %s, got: %s", expectedLoggingURL, updatedRev.Status.LogURL)
	}
}

// TODO(mattmoor): Remove when we have coverage of EnqueueEndpointsRevision
func TestMarkRevReadyUponEndpointBecomesReady(t *testing.T) {
	kubeClient, servingClient, cachingClient, _, controller, kubeInformer, servingInformer, cachingInformer, _, _ := newTestController(t, nil)
	rev := getTestRevision()

	h := NewHooks()
	// Look for the revision ready event. Events are delivered asynchronously so
	// we need to use hooks here.
	expectedMessage := "Revision becomes ready upon endpoint \"test-rev-service\" becoming ready"
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, expectedMessage))

	deployingRev := createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, controller, rev)

	// The revision is not marked ready until an endpoint is created.
	for _, ct := range []duckv1alpha1.ConditionType{"Ready"} {
		got := deployingRev.Status.GetCondition(ct)
		want := &duckv1alpha1.Condition{
			Type:               ct,
			Status:             corev1.ConditionUnknown,
			Reason:             "Deploying",
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
		}
	}

	endpoints := getTestReadyEndpoints(rev.Name)
	kubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(endpoints)
	kpa := getTestReadyKPA(rev)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)
	f := controller.EnqueueLabelOfNamespaceScopedResource("", serving.RevisionLabelKey)
	f(endpoints)
	if err := controller.Reconciler.Reconcile(context.TODO(), KeyOrDie(rev)); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	// Make sure that the changes from the Reconcile are reflected in our Informers.
	readyRev, _, _ := addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, rev)

	// After reconciling the endpoint, the revision should be ready.
	for _, ct := range []duckv1alpha1.ConditionType{"Ready"} {
		got := readyRev.Status.GetCondition(ct)
		want := &duckv1alpha1.Condition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: got.LastTransitionTime,
			Severity:           "Error",
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

func TestNoQueueSidecarImageUpdateFail(t *testing.T) {
	kubeClient, servingClient, cachingClient, _, controller, kubeInformer, servingInformer, cachingInformer, watcher, _ := newTestController(t, nil)

	rev := getTestRevision()
	config := getTestConfiguration()
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
	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, controller, rev)

	// Look for the revision deployment.
	_, err := kubeClient.AppsV1().Deployments(system.Namespace()).Get(rev.Name, metav1.GetOptions{})
	if !apierrs.IsNotFound(err) {
		t.Errorf("Expected revision deployment %s to not exist.", rev.Name)
	}
}

// This covers *error* paths in receiveNetworkConfig, since "" is not a valid value.
func TestIstioOutboundIPRangesInjection(t *testing.T) {
	var annotations map[string]string

	// A valid IP range
	in := "  10.10.10.0/24\r,,\t,\n,,"
	want := "10.10.10.0/24"
	annotations = getPodAnnotationsForConfig(t, in, "")
	if got := annotations[resources.IstioOutboundIPRangeAnnotation]; want != got {
		t.Fatalf("%v annotation expected to be %v, but is %v.", resources.IstioOutboundIPRangeAnnotation, want, got)
	}

	// Multiple valid ranges with whitespaces
	in = " \t\t10.10.10.0/24,  ,,\t\n\r\n,10.240.10.0/14\n,   192.192.10.0/16"
	want = "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16"
	annotations = getPodAnnotationsForConfig(t, in, "")
	if got := annotations[resources.IstioOutboundIPRangeAnnotation]; want != got {
		t.Fatalf("%v annotation expected to be %v, but is %v.", resources.IstioOutboundIPRangeAnnotation, want, got)
	}

	// An invalid IP range
	in = "10.10.10.10/33"
	annotations = getPodAnnotationsForConfig(t, in, "")
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

func getPodAnnotationsForConfig(t *testing.T, configMapValue string, configAnnotationOverride string) map[string]string {
	controllerConfig := getTestControllerConfig()
	kubeClient, servingClient, cachingClient, _, controller, kubeInformer, servingInformer, cachingInformer, watcher, _ := newTestControllerWithConfig(t, controllerConfig)

	// Resolve image references to this "digest"
	digest := "foo@sha256:deadbeef"
	controller.Reconciler.(*Reconciler).resolver = &fixedResolver{digest}

	watcher.OnChange(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.NetworkConfigName,
			Namespace: system.Namespace(),
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
		*kmeta.NewControllerRef(config),
	)

	createRevision(t, kubeClient, kubeInformer, servingClient, servingInformer, cachingClient, cachingInformer, controller, rev)

	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get serving deployment: %v", err)
	}
	return deployment.Spec.Template.ObjectMeta.Annotations
}

func TestGlobalResyncOnConfigMapUpdate(t *testing.T) {

	// Test that changes to the ConfigMap result in the desired changes on an existing
	// deployment and revision.
	tests := []struct {
		name              string
		expected          string
		configMapToUpdate corev1.ConfigMap
		wasUpdated        func(string, *v1alpha1.Revision, *appsv1.Deployment) (string, bool)
	}{{
		name:     "Update Istio Outbound IP Ranges", // Should update metadata on Deployment
		expected: "10.0.0.1/24",
		configMapToUpdate: corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.NetworkConfigName,
				Namespace: system.Namespace,
			},
			Data: map[string]string{
				"istio.sidecar.includeOutboundIPRanges": "10.0.0.1/24",
			},
		},
		wasUpdated: func(expected string, revision *v1alpha1.Revision, deployment *appsv1.Deployment) (string, bool) {
			annotations := deployment.Spec.Template.ObjectMeta.Annotations
			got := annotations[resources.IstioOutboundIPRangeAnnotation]
			return got, (got == expected)
		},
	}, {
		name:     "Disable Fluentd", // Should remove fluentd from Deployment
		expected: "",
		configMapToUpdate: corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      config.ObservabilityConfigName,
			},
			Data: map[string]string{
				"logging.enable-var-log-collection": "false",
			},
		},
		wasUpdated: func(expected string, revision *v1alpha1.Revision, deployment *appsv1.Deployment) (string, bool) {
			for _, c := range deployment.Spec.Template.Spec.Containers {
				if c.Name == resources.FluentdContainerName {
					return c.Image, false
				}
			}
			return "", true
		},
	}, {
		name:     "Update LoggingURL", // Should update LogURL on revision
		expected: "http://log-here.test.com?filter=",
		configMapToUpdate: corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      config.ObservabilityConfigName,
			},
			Data: map[string]string{
				"logging.enable-var-log-collection":     "true",
				"logging.fluentd-sidecar-image":         testFluentdImage,
				"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
				"logging.revision-url-template":         "http://log-here.test.com?filter=${REVISION_UID}",
			},
		},
		wasUpdated: func(expected string, revision *v1alpha1.Revision, deployment *appsv1.Deployment) (string, bool) {
			got := revision.Status.LogURL
			return got, strings.HasPrefix(got, expected)
		},
	}, {
		name:     "Update Fluentd Image", // Should Fluentd to Deployment
		expected: "newFluentdImage",
		configMapToUpdate: corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      config.ObservabilityConfigName,
			},
			Data: map[string]string{
				"logging.enable-var-log-collection":     "true",
				"logging.fluentd-sidecar-image":         "newFluentdImage",
				"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
			},
		},
		wasUpdated: func(expected string, revision *v1alpha1.Revision, deployment *appsv1.Deployment) (string, bool) {
			var got string
			for _, c := range deployment.Spec.Template.Spec.Containers {
				if c.Name == resources.FluentdContainerName {
					got = c.Image
					if got == expected {
						return got, true
					}
				}
			}
			return got, false
		},
	}, {
		name:     "Update QueueProxy Image", // Should update queueSidecarImage
		expected: "myAwesomeQueueImage",
		configMapToUpdate: corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      config.ControllerConfigName,
			},
			Data: map[string]string{
				"queueSidecarImage": "myAwesomeQueueImage",
			},
		},
		wasUpdated: func(expected string, revision *v1alpha1.Revision, deployment *appsv1.Deployment) (string, bool) {
			var got string
			for _, c := range deployment.Spec.Template.Spec.Containers {
				if c.Name == resources.QueueContainerName {
					got = c.Image
					if got == expected {
						return got, true
					}
				}
			}
			return got, false
		},
	}}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			controllerConfig := getTestControllerConfig()
			kubeClient, servingClient, _, _, controller, kubeInformer, servingInformer, cachingInformer, watcher, _ := newTestControllerWithConfig(t, controllerConfig)

			stopCh := make(chan struct{})
			defer close(stopCh)

			rev := getTestRevision()
			revClient := servingClient.ServingV1alpha1().Revisions(rev.Namespace)
			deploymentsClient := kubeClient.Apps().Deployments(rev.Namespace)
			h := NewHooks()

			h.OnUpdate(&servingClient.Fake, "revisions", func(obj runtime.Object) HookResult {
				updatedRev := obj.(*v1alpha1.Revision)
				t.Logf("Revision updated: %v", updatedRev.Name)
				updatedDeployment, err := deploymentsClient.Get(resourcenames.Deployment(updatedRev), metav1.GetOptions{})
				if err != nil {
					t.Error(err)
				}

				got, wasUpdated := test.wasUpdated(test.expected, updatedRev, updatedDeployment)

				if !wasUpdated {
					t.Logf("No update occurred. expected: %s got: %s", test.expected, got)
					return HookIncomplete
				}

				//Look for expected change
				return HookComplete
			})

			servingInformer.Start(stopCh)
			kubeInformer.Start(stopCh)
			cachingInformer.Start(stopCh)

			servingInformer.WaitForCacheSync(stopCh)
			kubeInformer.WaitForCacheSync(stopCh)
			cachingInformer.WaitForCacheSync(stopCh)

			if err := watcher.Start(stopCh); err != nil {
				t.Fatalf("Failed to start configuration manager: %v", err)
			}

			go controller.Run(1, stopCh)

			revClient.Create(rev)

			watcher.OnChange(&test.configMapToUpdate)

			if err := h.WaitForHooks(time.Second); err != nil {
				t.Errorf("%s Global Resync Failed: %v", test.name, err)
			}
		})
	}
}
