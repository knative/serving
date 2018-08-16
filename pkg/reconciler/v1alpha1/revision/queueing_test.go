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
	"testing"
	"time"

	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	buildinformers "github.com/knative/build/pkg/client/informers/externalversions"
	"github.com/knative/pkg/configmap"
	ctrl "github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/logging"
	rclr "github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"github.com/knative/serving/pkg/system"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakevpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vpainformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/pkg/logging/testing"
	. "github.com/knative/serving/pkg/reconciler/testing"
)

type nopResolver struct{}

func (r *nopResolver) Resolve(_ *appsv1.Deployment) error {
	return nil
}

const (
	testAutoscalerImage            = "autoscalerImage"
	testFluentdImage               = "fluentdImage"
	testFluentdSidecarOutputConfig = `
<match **>
  @type elasticsearch
</match>
`
	testNamespace  = "test"
	testQueueImage = "queueImage"
)

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/revisions/test-rev",
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
			ServingState:     v1alpha1.RevisionServingStateActive,
			ConcurrencyModel: v1alpha1.RevisionRequestConcurrencyModelMulti,
		},
	}
}

func getTestControllerConfig() *config.Controller {
	c, _ := config.NewControllerConfigFromConfigMap(getTestControllerConfigMap())
	// ignoring error as test controller is generated
	return c
}

func getTestControllerConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.ControllerConfigName,
			Namespace: system.Namespace,
		},
		Data: map[string]string{
			"queueSidecarImage": testQueueImage,
			"autoscalerImage":   testAutoscalerImage,
		},
	}
}

func newTestController(t *testing.T, servingObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	buildClient *fakebuildclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	vpaClient *fakevpaclientset.Clientset,
	controller *ctrl.Impl,
	kubeInformer kubeinformers.SharedInformerFactory,
	buildInformer buildinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	configMapWatcher configmap.Watcher,
	vpaInformer vpainformers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	buildClient = fakebuildclientset.NewSimpleClientset()
	servingClient = fakeclientset.NewSimpleClientset(servingObjects...)
	vpaClient = fakevpaclientset.NewSimpleClientset()

	configMapWatcher = configmap.NewFixedWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      config.NetworkConfigName,
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      logging.ConfigName,
		},
		Data: map[string]string{
			"zap-logger-config":   "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}",
			"loglevel.queueproxy": "info",
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      config.ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         testFluentdImage,
			"logging.fluentd-sidecar-output-config": testFluentdSidecarOutputConfig,
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
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
			"tick-interval":               "2s",
		},
	}, getTestControllerConfigMap())

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	buildInformer = buildinformers.NewSharedInformerFactory(buildClient, 0)
	servingInformer = informers.NewSharedInformerFactory(servingClient, 0)
	vpaInformer = vpainformers.NewSharedInformerFactory(vpaClient, 0)

	controller = NewController(
		rclr.Options{
			KubeClientSet:    kubeClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           TestLogger(t),
		},
		vpaClient,
		servingInformer.Serving().V1alpha1().Revisions(),
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		buildInformer.Build().V1alpha1().Builds(),
		kubeInformer.Apps().V1().Deployments(),
		kubeInformer.Core().V1().Services(),
		kubeInformer.Core().V1().Endpoints(),
		kubeInformer.Core().V1().ConfigMaps(),
		vpaInformer.Poc().V1alpha1().VerticalPodAutoscalers(),
	)

	controller.Reconciler.(*Reconciler).resolver = &nopResolver{}

	return
}

func TestNewRevisionCallsSyncHandler(t *testing.T) {
	rev := getTestRevision()
	// TODO(grantr): inserting the route at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.
	kubeClient, _, _, _, controller, kubeInformer, buildInformer,
		servingInformer, servingSystemInformer, vpaInformer :=
		newTestController(t, rev)

	h := NewHooks()

	// Check for a service created as a signal that syncHandler ran
	h.OnCreate(&kubeClient.Fake, "services", func(obj runtime.Object) HookResult {
		service := obj.(*corev1.Service)
		t.Logf("service created: %q", service.Name)

		return HookComplete
	})

	stopCh := make(chan struct{})
	defer close(stopCh)

	kubeInformer.Start(stopCh)
	buildInformer.Start(stopCh)
	servingInformer.Start(stopCh)
	servingSystemInformer.Start(stopCh)
	vpaInformer.Start(stopCh)

	go func() {
		if err := controller.Run(2, stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
