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

package configuration

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/system"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	"knative.dev/serving/pkg/gc"

	. "knative.dev/pkg/reconciler/testing"
)

const (
	testNamespace = "test"
)

func getTestConfiguration() *v1alpha1.Configuration {
	cfg := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/configurations/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Template: &v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					RevisionSpec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							ServiceAccountName: "test-account",
							// corev1.Container has a lot of setting.  We try to pass many
							// of them here to verify that we pass through the settings to
							// the derived Revisions.
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
									TimeoutSeconds: 43,
								},
								TerminationMessagePath: "/dev/null",
							}},
						},
					},
				},
			},
		},
	}
	cfg.SetDefaults(context.Background())
	return cfg
}

func TestNewConfigurationCallsSyncHandler(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)

	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	})

	ctrl := NewController(ctx, configMapWatcher)

	eg := errgroup.Group{}
	defer func() {
		cancel()
		if err := eg.Wait(); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
		logtesting.ClearAll()
	}()

	servingClient := fakeservingclient.Get(ctx)

	h := NewHooks()

	// Check for revision created as a signal that syncHandler ran.
	h.OnCreate(&servingClient.Fake, "revisions", func(obj runtime.Object) HookResult {
		rev := obj.(*v1alpha1.Revision)
		t.Logf("Revision created: %q", rev.Name)

		return HookComplete
	})

	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		t.Fatalf("Failed to start cluster ingress manager: %v", err)
	}

	eg.Go(func() error {
		return ctrl.Run(2, ctx.Done())
	})

	config := getTestConfiguration()
	if _, err := servingClient.ServingV1alpha1().Configurations(config.Namespace).Create(config); err != nil {
		t.Fatalf("Unexpected error creating configuration: %v", err)
	}

	if err := h.WaitForHooks(5 * time.Second); err != nil {
		t.Error(err)
	}
}
