//go:build e2e
// +build e2e

/*
Copyright 2019 The Knative Authors

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

package e2e

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/ptr"
	pkgtest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	revisionnames "knative.dev/serving/pkg/reconciler/revision/resources/names"
)

func TestDefectiveRevisionScalesDown(t *testing.T) {
	t.Parallel()
	clients := Setup(t) // This one uses the default namespace `test.ServingFlags.TestNamespace`

	resources := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.RevisionFailure,
	}
	test.EnsureTearDown(t, clients, &resources)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: resources.Service,
		},
	}
	ctx := context.Background()

	serviceFunc := func(s *v1.Service) {
		name := time.Now().String()

		s.Spec.Template.Spec.TimeoutSeconds = ptr.Int64(30)
		s.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{
			Name:  "NAME",
			Value: name,
		}}
		s.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
			Name:      "failure-list",
			MountPath: "/etc/config",
		}}
		s.Spec.Template.Spec.Volumes = []corev1.Volume{{
			Name: "failure-list",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: resources.Service,
					},
				},
			},
		}}
	}

	cmClient := clients.KubeClient.CoreV1().ConfigMaps(test.ServingFlags.TestNamespace)
	if _, err := cmClient.Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatal("failed to create configmap", err)
	}

	t.Cleanup(func() {
		cmClient.Delete(ctx, resources.Service, metav1.DeleteOptions{})
	})

	objs, err := v1test.CreateServiceReady(t, clients, &resources, serviceFunc)
	if err != nil {
		t.Fatalf("Failed to create Service %q in namespace %q: %v", resources.Service, test.ServingFlags.TestNamespace, err)
	}

	rev1DeploymentName := revisionnames.Deployment(objs.Revision)
	if err = WaitForScaleToZero(t, rev1DeploymentName, clients); err != nil {
		t.Fatalf("Failed to scale Service %q in namespace %q to zero: %v", resources.Service, test.ServingFlags.TestNamespace, err)
	}

	// Update the ConfigMap to trigger the first revision to fail
	cm.Data = map[string]string{resources.Revision: "fail"}

	if _, err := cmClient.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal("failed to trigger revision to fail", err)
	}

	url := objs.Service.Status.URL
	client, err := pkgtest.NewSpoofingClient(ctx,
		clients.KubeClient,
		t.Logf,
		url.Host,
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	)
	if err != nil {
		t.Fatal("failed to make spoof client")
	}

	// trigger scale from zero
	errs := make(chan error, 1)
	go func() {
		t.Log("making a request")

		now := time.Now()
		req, _ := http.NewRequest("GET", url.String(), nil)
		resp, err := client.Do(req)

		t.Log("making a request is done - took", time.Since(now))
		if err != nil {
			errs <- err
		} else if resp.StatusCode == http.StatusOK {
			errs <- errors.New("expect scale up request to fail")
		}
		close(errs)
	}()

	var requestErr error
	err = pkgtest.WaitForDeploymentState(
		context.Background(),
		clients.KubeClient,
		rev1DeploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			// Fail if there's an error making the request
			select {
			case requestErr = <-errs:
				return false, requestErr
			default:
			}
			return d.Status.Replicas == 1 && d.Status.UnavailableReplicas == 1, nil
		},
		"DeploymentIsScalingUp",
		test.ServingFlags.TestNamespace,
		2*time.Minute,
	)

	if requestErr != nil {
		t.Fatal("triggering request to revision failed: ", err)
	}

	if err != nil {
		t.Fatal("failed to wait for deployment to scale up: ", err)
	}

	if _, err = v1test.UpdateService(t, clients, resources, serviceFunc); err != nil {
		t.Fatal("failed to update service: ", err)
	}

	if _, err = v1test.WaitForServiceLatestRevision(clients, resources); err != nil {
		t.Fatal("failed to rollout new revision", err)
	}

	if err = WaitForScaleToZero(t, rev1DeploymentName, clients); err != nil {
		t.Fatal("first revision failed to scale down")
	}

	t.Fatal("it shouldn't fail here")
}
