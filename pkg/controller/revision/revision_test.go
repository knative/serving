/*
Copyright 2018 Google LLC.

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
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	buildv1alpha1 "github.com/elafros/elafros/pkg/apis/build/v1alpha1"
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/elafros/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	ctrl "github.com/elafros/elafros/pkg/controller"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	. "github.com/elafros/elafros/pkg/controller/testing"
)

const testNamespace string = "test"
const testFluentdImage string = "fluentdImage"
const testQueueImage string = "queueImage"
const testAutoscalerImage string = "autoscalerImage"

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      "test-rev",
			Namespace: testNamespace,
			Labels: map[string]string{
				"testLabel1":      "foo",
				"testLabel2":      "bar",
				ela.RouteLabelKey: "test-route",
			},
			Annotations: map[string]string{
				"testAnnotation": "test",
			},
		},
		Spec: v1alpha1.RevisionSpec{
			// corev1.Container has a lot of setting.  We try to pass many
			// of them here to verify that we pass through the settings to
			// derived objects.
			Container: corev1.Container{
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
			},
			ServingState: v1alpha1.RevisionServingStateActive,
		},
	}
}

func getTestConfiguration() *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/configurations/test-config",
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
				ela.RevisionLabelKey: revName,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					corev1.EndpointAddress{
						IP: "123.456.78.90",
					},
				},
			},
		},
	}
}

func getTestAuxiliaryReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-auxiliary", revName),
			Namespace: testNamespace,
			Labels: map[string]string{
				ela.RevisionLabelKey: revName,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					corev1.EndpointAddress{
						IP: "123.456.78.90",
					},
				},
			},
		},
	}
}

func getTestNotReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-service", revName),
			Namespace: testNamespace,
			Labels: map[string]string{
				ela.RevisionLabelKey: revName,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{},
			},
		},
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

func getTestControllerConfig() ControllerConfig {
	autoscaleConcurrencyQuantumOfTime := 100 * time.Millisecond
	return ControllerConfig{
		QueueSidecarImage:                 testQueueImage,
		AutoscalerImage:                   testAutoscalerImage,
		AutoscaleConcurrencyQuantumOfTime: k8sflag.Duration("autoscale.concurrency-quantum-of-time", &autoscaleConcurrencyQuantumOfTime),

		EnableVarLogCollection: true,
		FluentdSidecarImage:    testFluentdImage,
	}
}

func newTestControllerWithConfig(t *testing.T, controllerConfig *ControllerConfig, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset(elaObjects...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	controller = NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		&rest.Config{},
		controllerConfig,
	).(*Controller)

	return
}

func newTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory) {
	testControllerConfig := getTestControllerConfig()
	return newTestControllerWithConfig(t, &testControllerConfig, elaObjects...)
}

func newRunningTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	kubeClient, elaClient, controller, kubeInformer, elaInformer = newTestController(t, elaObjects...)

	// Start the informers. This must happen after the call to NewController,
	// otherwise there are no informers to be started.
	stopCh = make(chan struct{})
	kubeInformer.Start(stopCh)
	elaInformer.Start(stopCh)

	// Run the controller.
	go func() {
		if err := controller.Run(2, stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	return
}

func compareRevisionConditions(want []v1alpha1.RevisionCondition, got []v1alpha1.RevisionCondition) string {
	for i := range got {
		got[i].LastTransitionTime = metav1.NewTime(time.Time{})
	}
	return cmp.Diff(want, got)
}

func createRevision(elaClient *fakeclientset.Clientset, elaInformer informers.SharedInformerFactory, controller *Controller, rev *v1alpha1.Revision) {
	elaClient.ElafrosV1alpha1().Revisions(rev.Namespace).Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	controller.syncHandler(KeyOrDie(rev))
}

func updateRevision(elaClient *fakeclientset.Clientset, elaInformer informers.SharedInformerFactory, controller *Controller, rev *v1alpha1.Revision) {
	elaClient.ElafrosV1alpha1().Revisions(rev.Namespace).Update(rev)
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Update(rev)

	controller.syncHandler(KeyOrDie(rev))
}

func TestCreateRevCreatesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()
	config := getTestConfiguration()
	rev.OwnerReferences = append(
		rev.OwnerReferences,
		*ctrl.NewConfigurationControllerRef(config),
	)

	createRevision(elaClient, elaInformer, controller, rev)

	// This function is used to verify pass through of container environment
	// variables.
	checkEnv := func(env []corev1.EnvVar, name, value, fieldPath string) {
		nameFound := false
		for _, e := range env {
			if e.Name == name {
				nameFound = true
				if value != "" && e.Value != value {
					t.Errorf("Incorrect environment variable %s. Expected value %s. Got %s.", name, value, e.Value)
				}
				if fieldPath != "" {
					if vf := e.ValueFrom; vf == nil {
						t.Errorf("Incorrect environment variable %s. Missing value source.", name)
					} else if fr := vf.FieldRef; fr == nil {
						t.Errorf("Incorrect environment variable %s. Missing field ref.", name)
					} else if fr.FieldPath != fieldPath {
						t.Errorf("Incorrect environment variable %s. Expected field path %s. Got %s.",
							name, fr.FieldPath, fieldPath)
					}
				}
			}
		}
		if !nameFound {
			t.Errorf("Missing environment variable %s", name)
		}
	}

	// Look for the revision deployment.
	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}

	if len(deployment.OwnerReferences) != 1 && rev.Name != deployment.OwnerReferences[0].Name {
		t.Errorf("expected owner references to have 1 ref with name %s", rev.Name)
	}

	foundQueueProxy := false
	foundElaContainer := false
	foundFluentdProxy := false
	expectedPreStop := &corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port: intstr.FromInt(RequestQueueAdminPort),
			Path: RequestQueueQuitPath,
		},
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "queue-proxy" {
			foundQueueProxy = true
			checkEnv(container.Env, "ELA_NAMESPACE", testNamespace, "")
			checkEnv(container.Env, "ELA_REVISION", "test-rev", "")
			checkEnv(container.Env, "ELA_POD", "", "metadata.name")
			if diff := cmp.Diff(expectedPreStop, container.Lifecycle.PreStop); diff != "" {
				t.Errorf("Unexpected PreStop diff in container %q (-want +got): %v", container.Name, diff)
			}
		}
		if container.Name == elaContainerName {
			foundElaContainer = true
			// verify that the ReadinessProbe has our port.
			if container.ReadinessProbe.Handler.HTTPGet.Port != intstr.FromInt(RequestQueuePort) {
				t.Errorf("Expect ReadinessProbe handler to have port %d, saw %v",
					RequestQueuePort, container.ReadinessProbe.Handler.HTTPGet.Port)
			}
			if diff := cmp.Diff(expectedPreStop, container.Lifecycle.PreStop); diff != "" {
				t.Errorf("Unexpected PreStop diff in container %q (-want +got): %v", container.Name, diff)
			}
		}
		if container.Name == "fluentd-proxy" {
			foundFluentdProxy = true
			checkEnv(container.Env, "ELA_NAMESPACE", testNamespace, "")
			checkEnv(container.Env, "ELA_REVISION", "test-rev", "")
			checkEnv(container.Env, "ELA_CONFIGURATION", "test-config", "")
			checkEnv(container.Env, "ELA_CONTAINER_NAME", "ela-container", "")
			checkEnv(container.Env, "ELA_POD_NAME", "", "metadata.name")
		}
	}
	if !foundQueueProxy {
		t.Error("Missing queue-proxy container")
	}
	if !foundFluentdProxy {
		t.Error("Missing fluentd-proxy container")
	}
	if !foundElaContainer {
		t.Errorf("Missing %q container", elaContainerName)
	}
	expectedLabels := sumMaps(
		rev.Labels,
		map[string]string{
			ela.RevisionLabelKey: rev.Name,
			appLabelKey:          rev.Name,
		},
	)
	expectedAnnotations := rev.Annotations
	expectedPodSpecAnnotations := sumMaps(
		rev.Annotations,
		map[string]string{"sidecar.istio.io/inject": "true"},
	)

	if labels := deployment.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("Labels not set correctly on deployment: expected %v got %v.",
			expectedLabels, labels)
	}
	if labels := deployment.Spec.Template.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("Label not set correctly in pod template: expected %v got %v.",
			expectedLabels, labels)
	}
	if annotations := deployment.ObjectMeta.Annotations; !reflect.DeepEqual(annotations, expectedAnnotations) {
		t.Errorf("Annotations not set correctly on deployment: expected %v got %v.",
			expectedAnnotations, annotations)
	}
	if annotations := deployment.Spec.Template.ObjectMeta.Annotations; !reflect.DeepEqual(annotations, expectedPodSpecAnnotations) {
		t.Errorf("Annotations not set correctly in pod template: expected %v got %v.",
			expectedPodSpecAnnotations, annotations)
	}

	// Look for the revision service.
	expectedServiceName := fmt.Sprintf("%s-service", rev.Name)
	service, err := kubeClient.CoreV1().Services(testNamespace).Get(expectedServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision service: %v", err)
	}
	// The revision service should be owned by rev.
	expectedRefs := []metav1.OwnerReference{
		metav1.OwnerReference{
			APIVersion: "elafros.dev/v1alpha1",
			Kind:       "Revision",
			Name:       rev.Name,
		},
	}

	if diff := cmp.Diff(expectedRefs, service.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected service owner refs diff (-want +got): %v", diff)
	}
	if labels := service.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("Label not set correctly for revision service: expected %v got %v.",
			expectedLabels, labels)
	}
	if annotations := service.ObjectMeta.Annotations; !reflect.DeepEqual(annotations, expectedAnnotations) {
		t.Errorf("Annotations not set correctly for revision service: expected %v got %v.",
			expectedAnnotations, annotations)
	}

	// Look for the autoscaler deployment.
	expectedAutoscalerName := fmt.Sprintf("%s-autoscaler", rev.Name)
	expectedAutoscalerLabels := sumMaps(
		expectedLabels,
		map[string]string{ela.AutoscalerLabelKey: expectedAutoscalerName},
	)
	expectedAutoscalerPodSpecAnnotations := sumMaps(
		rev.Annotations,
		map[string]string{"sidecar.istio.io/inject": "false"},
	)

	asDeployment, err := kubeClient.AppsV1().Deployments(AutoscalerNamespace).Get(expectedAutoscalerName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get autoscaler deployment: %v", err)
	}
	if labels := asDeployment.Spec.Template.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedAutoscalerLabels) {
		t.Errorf("Label not set correctly in autoscaler pod template: expected %v got %v.",
			expectedAutoscalerLabels, labels)
	}
	if annotations := asDeployment.Spec.Template.ObjectMeta.Annotations; !reflect.DeepEqual(annotations, expectedAutoscalerPodSpecAnnotations) {
		t.Errorf("Annotations not set correctly in autoscaler pod template: expected %v got %v.",
			expectedAutoscalerPodSpecAnnotations, annotations)
	}
	// Check the autoscaler deployment environment variables
	foundAutoscaler := false
	for _, container := range asDeployment.Spec.Template.Spec.Containers {
		if container.Name == "autoscaler" {
			foundAutoscaler = true
			checkEnv(container.Env, "ELA_NAMESPACE", testNamespace, "")
			checkEnv(container.Env, "ELA_DEPLOYMENT", expectedDeploymentName, "")
			checkEnv(container.Env, "ELA_CONFIGURATION", config.Name, "")
			checkEnv(container.Env, "ELA_REVISION", rev.Name, "")
			checkEnv(container.Env, "ELA_AUTOSCALER_PORT", strconv.Itoa(autoscalerPort), "")
			break
		}
	}
	if !foundAutoscaler {
		t.Error("Missing autoscaler")
	}

	// Look for the autoscaler service.
	asService, err := kubeClient.CoreV1().Services(AutoscalerNamespace).Get(expectedAutoscalerName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get autoscaler service: %v", err)
	}
	// The autoscaler service should also be owned by rev.
	if diff := cmp.Diff(expectedRefs, asService.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected service owner refs diff (-want +got): %v", diff)
	}
	if labels := asService.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedAutoscalerLabels) {
		t.Errorf("Label not set correctly autoscaler service: expected %v got %v.",
			expectedLabels, labels)
	}
	if annotations := asService.ObjectMeta.Annotations; !reflect.DeepEqual(annotations, expectedAnnotations) {
		t.Errorf("Annotations not set correctly autoscaler service: expected %v got %v.",
			expectedAnnotations, annotations)
	}

	rev, err = elaClient.ElafrosV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Look for the config map.
	configMap, err := kubeClient.Core().ConfigMaps(testNamespace).Get(fluentdConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config map: %v", err)
	}
	// The config map should also be owned by rev.
	if diff := cmp.Diff(expectedRefs, configMap.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected config map owner refs diff (-want +got): %v", diff)
	}
	if labels := configMap.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("Label not set correctly config map: expected %v got %v.",
			expectedLabels, labels)
	}
	if got, want := configMap.Data["varlog.conf"], fluentdConfigSource; got != want {
		t.Errorf("Fluent config file not set correctly config map: expected %v got %v.",
			want, got)
	}

	rev, err = elaClient.ElafrosV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Ensure that the Revision status is updated.
	want := []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionUnknown,
			Reason: "Deploying",
		},
	}
	if diff := compareRevisionConditions(want, rev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}
}

func TestCreateRevDoesNotSetUpFluentdSidecarIfVarLogCollectionDisabled(t *testing.T) {
	controllerConfig := getTestControllerConfig()
	controllerConfig.EnableVarLogCollection = false
	kubeClient, elaClient, controller, _, elaInformer := newTestControllerWithConfig(t, &controllerConfig)
	rev := getTestRevision()
	config := getTestConfiguration()
	rev.OwnerReferences = append(
		rev.OwnerReferences,
		*ctrl.NewConfigurationControllerRef(config),
	)

	createRevision(elaClient, elaInformer, controller, rev)

	// Look for the revision deployment.
	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}

	if len(deployment.OwnerReferences) != 1 && rev.Name != deployment.OwnerReferences[0].Name {
		t.Errorf("expected owner references to have 1 ref with name %s", rev.Name)
	}

	// Check the revision deployment fluentd proxy
	foundFluentdProxy := false
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "fluentd-proxy" {
			foundFluentdProxy = true
			break
		}
	}
	if foundFluentdProxy {
		t.Error("fluentd-proxy container shouldn't be set up")
	}

	// Look for the config map.
	_, err = kubeClient.Core().ConfigMaps(testNamespace).Get(fluentdConfigMapName, metav1.GetOptions{})
	if !apierrs.IsNotFound(err) {
		t.Fatalf("The ConfigMap %s shouldn't exist: %v", fluentdConfigMapName, err)
	}
}

func TestCreateRevPreservesAppLabel(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()
	rev.Labels[appLabelKey] = "app-label-that-should-stay-unchanged"
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	controller.syncHandler(KeyOrDie(rev))

	// Look for the revision deployment.
	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}
	expectedLabels := sumMaps(
		rev.Labels,
		map[string]string{
			ela.RevisionLabelKey: rev.Name,
		},
	)
	if labels := deployment.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("Labels not set correctly on deployment: expected %v got %v.",
			expectedLabels, labels)
	}
	if labels := deployment.Spec.Template.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("Label not set correctly in pod template: expected %v got %v.",
			expectedLabels, labels)
	}
	// Look for the revision service.
	expectedServiceName := fmt.Sprintf("%s-service", rev.Name)
	service, err := kubeClient.CoreV1().Services(testNamespace).Get(expectedServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision service: %v", err)
	}
	if labels := service.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("Label not set correctly for revision service: expected %v got %v.",
			expectedLabels, labels)
	}
	// Look for the autoscaler deployment.
	expectedAutoscalerName := fmt.Sprintf("%s-autoscaler", rev.Name)
	expectedAutoscalerLabels := sumMaps(
		expectedLabels,
		map[string]string{ela.AutoscalerLabelKey: expectedAutoscalerName},
	)
	asDeployment, err := kubeClient.AppsV1().Deployments(AutoscalerNamespace).Get(expectedAutoscalerName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get autoscaler deployment: %v", err)
	}
	if labels := asDeployment.Spec.Template.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedAutoscalerLabels) {
		t.Errorf("Label not set correctly in autoscaler pod template: expected %v got %v.",
			expectedAutoscalerLabels, labels)
	}
	// Look for the autoscaler service.
	asService, err := kubeClient.CoreV1().Services(AutoscalerNamespace).Get(expectedAutoscalerName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get autoscaler service: %v", err)
	}
	if labels := asService.ObjectMeta.Labels; !reflect.DeepEqual(labels, expectedAutoscalerLabels) {
		t.Errorf("Label not set correctly autoscaler service: expected %v got %v.",
			expectedLabels, labels)
	}
}

func TestCreateRevWithBuildNameWaits(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

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
	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name

	createRevision(elaClient, elaInformer, controller, rev)

	waitRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Ensure that the Revision status is updated.
	want := []v1alpha1.RevisionCondition{
		{
			Type:   "BuildSucceeded",
			Status: corev1.ConditionUnknown,
			Reason: "Building",
		},
	}
	if diff := compareRevisionConditions(want, waitRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}
}

func TestCreateRevWithFailedBuildNameFails(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	reason := "Foo"
	errMessage := "a long human-readable error message."

	h := NewHooks()
	// Look for the build failure event. Events are delivered asynchronously so
	// we need to use hooks here.
	h.OnCreate(&kubeClient.Fake, "events", ExpectWarningEventDelivery(t, errMessage))

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
	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	controller.syncHandler(KeyOrDie(rev))

	// After the initial update to the revision, we should be
	// watching for this build to complete, so make it complete, but
	// with a failure.
	bld.Status = buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{
			{
				Type:    buildv1alpha1.BuildFailed,
				Status:  corev1.ConditionTrue,
				Reason:  reason,
				Message: errMessage,
			},
		},
	}

	controller.addBuildEvent(bld)

	failedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// The next update we receive should tell us that the build failed,
	// and surface the reason and message from that failure in our own
	// status.
	want := []v1alpha1.RevisionCondition{
		{
			Type:    "BuildSucceeded",
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: errMessage,
		},
		{
			Type:    "Ready",
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: errMessage,
		},
	}
	if diff := compareRevisionConditions(want, failedRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithCompletedBuildNameCompletes(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	h := NewHooks()
	// Look for the build complete event. Events are delivered asynchronously so
	// we need to use hooks here.
	h.OnCreate(&kubeClient.Fake, "events", func(obj runtime.Object) HookResult {
		event := obj.(*corev1.Event)
		if wanted, got := "BuildComplete", event.Reason; wanted != got {
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
	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	controller.syncHandler(KeyOrDie(rev))

	// After the initial update to the revision, we should be
	// watching for this build to complete, so make it complete.
	bld.Status = buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{
			{
				Type:    buildv1alpha1.BuildComplete,
				Status:  corev1.ConditionTrue,
				Message: completeMessage,
			},
		},
	}

	controller.addBuildEvent(bld)

	completedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// The next update we receive should tell us that the build completed.
	want := []v1alpha1.RevisionCondition{
		{
			Type:   "BuildSucceeded",
			Status: corev1.ConditionTrue,
		},
	}
	if diff := compareRevisionConditions(want, completedRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithInvalidBuildNameFails(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	reason := "Foo"
	errMessage := "a long human-readable error message."

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

	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	controller.syncHandler(KeyOrDie(rev))

	// After the initial update to the revision, we should be
	// watching for this build to complete, so make it complete, but
	// with a validation failure.
	bld.Status = buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{
			{
				Type:    buildv1alpha1.BuildInvalid,
				Status:  corev1.ConditionTrue,
				Reason:  reason,
				Message: errMessage,
			},
		},
	}

	controller.addBuildEvent(bld)

	failedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	want := []v1alpha1.RevisionCondition{
		{
			Type:    "BuildSucceeded",
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: errMessage,
		},
		{
			Type:    "Ready",
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: errMessage,
		},
	}
	if diff := compareRevisionConditions(want, failedRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

}

func TestCreateRevWithProgressDeadlineSecondsStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	var testProgressDeadlineSeconds int32 = 10

	rev := getTestRevision()

	revClient.Create(rev)

	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	controller.syncHandler(KeyOrDie(rev))

	// Look for revision's deployment.
	deploymentNameToLook := fmt.Sprintf("%s-deployment", rev.Name)

	deployment, err := kubeClient.Apps().Deployments(testNamespace).Get(deploymentNameToLook, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}

	if len(deployment.OwnerReferences) != 1 && rev.Name != deployment.OwnerReferences[0].Name {
		t.Errorf("expected owner references to have 1 ref with name %s", rev.Name)
	}

	//set ProgressDeadlineSeconds on Dep spec
	deployment.Spec.ProgressDeadlineSeconds = &testProgressDeadlineSeconds
	controller.addDeploymentProgressEvent(deployment)

	rev2Inspect, err := revClient.Get(rev.Name, metav1.GetOptions{})

	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	want := []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionUnknown,
			Reason: "Deploying",
		},
	}
	if diff := compareRevisionConditions(want, rev2Inspect.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}
}

func TestMarkRevReadyUponEndpointBecomesReady(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)
	rev := getTestRevision()

	h := NewHooks()
	// Look for the revision ready event. Events are delivered asynchronously so
	// we need to use hooks here.
	expectedMessage := "Revision becomes ready upon endpoint \"test-rev-service\" becoming ready"
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, expectedMessage))

	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	controller.syncHandler(KeyOrDie(rev))

	deployingRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// The revision is not marked ready until an endpoint is created.
	deployingConditions := []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionUnknown,
			Reason: "Deploying",
		},
	}
	if diff := compareRevisionConditions(deployingConditions, deployingRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	endpoints := getTestReadyEndpoints(rev.Name)
	controller.addEndpointsEvent(endpoints)

	readyRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// After reconciling the endpoint, the revision should be ready.
	readyConditions := []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionTrue,
			Reason: "ServiceReady",
		},
	}
	if diff := compareRevisionConditions(readyConditions, readyRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestDoNotUpdateRevIfRevIsAlreadyReady(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()
	// Mark the revision already ready.
	rev.Status.Conditions = []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionTrue,
			Reason: "ServiceReady",
		},
	}

	createRevision(elaClient, elaInformer, controller, rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestReadyEndpoints(rev.Name)

	// No revision updates.
	elaClient.Fake.PrependReactor("update", "revisions",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Revision was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.addEndpointsEvent(endpoints)
}

func TestDoNotUpdateRevIfRevIsMarkedAsFailed(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()
	// Mark the revision already ready.
	rev.Status.Conditions = []v1alpha1.RevisionCondition{
		v1alpha1.RevisionCondition{
			Type:   "ResourcesAvailable",
			Status: corev1.ConditionFalse,
			Reason: "ExceededReadinessChecks",
		},
		v1alpha1.RevisionCondition{
			Type:   "Ready",
			Status: corev1.ConditionFalse,
			Reason: "ExceededReadinessChecks",
		},
	}

	createRevision(elaClient, elaInformer, controller, rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestReadyEndpoints(rev.Name)

	// No revision updates.
	elaClient.Fake.PrependReactor("update", "revisions",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Revision was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.addEndpointsEvent(endpoints)
}

func TestMarkRevAsFailedIfEndpointHasNoAddressesAfterSomeDuration(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	creationTime := time.Now().Add(-10 * time.Minute)
	rev.ObjectMeta.CreationTimestamp = metav1.NewTime(creationTime)
	rev.Status.Conditions = []v1alpha1.RevisionCondition{
		v1alpha1.RevisionCondition{
			Type:   "Ready",
			Status: corev1.ConditionUnknown,
			Reason: "Deploying",
		},
	}

	createRevision(elaClient, elaInformer, controller, rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestNotReadyEndpoints(rev.Name)

	controller.addEndpointsEvent(endpoints)

	currentRev, _ := elaClient.ElafrosV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})

	want := []v1alpha1.RevisionCondition{
		{
			Type:    "ResourcesAvailable",
			Status:  corev1.ConditionFalse,
			Reason:  "ServiceTimeout",
			Message: "Timed out waiting for a service endpoint to become ready",
		},
		{
			Type:    "Ready",
			Status:  corev1.ConditionFalse,
			Reason:  "ServiceTimeout",
			Message: "Timed out waiting for a service endpoint to become ready",
		},
	}
	if diff := compareRevisionConditions(want, currentRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}
}

func TestAuxiliaryEndpointDoesNotUpdateRev(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	createRevision(elaClient, elaInformer, controller, rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestAuxiliaryReadyEndpoints(rev.Name)

	// No revision updates.
	elaClient.Fake.PrependReactor("update", "revisions",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Revision was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.addEndpointsEvent(endpoints)
}

func TestActiveToRetiredRevisionDeletesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	// Create revision and verify that the k8s resources are created as
	// appropriate.
	createRevision(elaClient, elaInformer, controller, rev)

	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	_, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}

	// Now, update the revision serving state to Retired, and force another
	// run of the controller.
	rev.Spec.ServingState = v1alpha1.RevisionServingStateRetired
	updateRevision(elaClient, elaInformer, controller, rev)

	// Expect the deployment to be gone.
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})

	if err == nil {
		t.Fatalf("Expected ela deployment to be missing but it was really here: %v", deployment)
	}
}

func TestActiveToReserveRevisionDeletesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	// Create revision and verify that the k8s resources are created as
	// appropriate.
	createRevision(elaClient, elaInformer, controller, rev)

	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	_, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}

	// Now, update the revision serving state to Reserve, and force another
	// run of the controller.
	rev.Spec.ServingState = v1alpha1.RevisionServingStateReserve
	updateRevision(elaClient, elaInformer, controller, rev)

	// Expect the deployment to be gone.
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err == nil {
		t.Fatalf("Expected ela deployment to be missing but it was really here: %v", deployment)
	}
}

func TestRetiredToActiveRevisionCreatesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	// Create revision. The k8s resources should not be created.
	rev.Spec.ServingState = v1alpha1.RevisionServingStateRetired
	createRevision(elaClient, elaInformer, controller, rev)

	// Expect the deployment to be gone.
	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err == nil {
		t.Fatalf("Expected ela deployment to be missing but it was really here: %v", deployment)
	}

	// Now, update the revision serving state to Active, and force another
	// run of the controller.
	rev.Spec.ServingState = v1alpha1.RevisionServingStateActive
	updateRevision(elaClient, elaInformer, controller, rev)

	// Expect the resources to be created.
	_, err = kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}
}

func TestReserveToActiveRevisionCreatesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	// Create revision. The k8s resources should not be created.
	rev.Spec.ServingState = v1alpha1.RevisionServingStateReserve
	createRevision(elaClient, elaInformer, controller, rev)

	// Expect the deployment to be gone.
	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err == nil {
		t.Fatalf("Expected ela deployment to be missing but it was really here: %v", deployment)
	}

	// Now, update the revision serving state to Active, and force another
	// run of the controller.
	rev.Spec.ServingState = v1alpha1.RevisionServingStateActive
	updateRevision(elaClient, elaInformer, controller, rev)

	// Expect the resources to be created.
	_, err = kubeClient.AppsV1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}
}
