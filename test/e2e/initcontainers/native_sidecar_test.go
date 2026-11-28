//go:build e2e

/*
Copyright 2026 The Knative Authors

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

package initcontainers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestNativeSidecarStartup(t *testing.T) {
	testNativeSidecar(t, false)
}

func TestNativeSidecarStartupFailure(t *testing.T) {
	testNativeSidecar(t, true)
}

func testNativeSidecar(t *testing.T, fail bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	clients := test.Setup(t)
	names := test.ResourceNames{Service: test.ObjectNameForTest(t), Image: test.Readiness}
	helper := corev1.Container{
		Name: "configuration-helper", Image: pkgTest.ImagePath(test.Readiness),
		RestartPolicy: new(corev1.ContainerRestartPolicyAlways),
		Env:           []corev1.EnvVar{{Name: "PORT", Value: "9000"}, {Name: "STARTUP_BLOCKED", Value: "true"}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		},
		StartupProbe: &corev1.Probe{
			ProbeHandler:  corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz/readiness", Port: intstr.FromInt(9000), Scheme: corev1.URISchemeHTTP}},
			PeriodSeconds: 1, TimeoutSeconds: 1, FailureThreshold: 120, SuccessThreshold: 1,
		},
	}
	if fail {
		helper.StartupProbe.FailureThreshold = 3
	}
	svc, err := v1test.CreateService(t, clients, names, rtesting.WithInitContainer(helper), func(s *v1.Service) {
		s.Spec.Template.Annotations = map[string]string{"autoscaling.knative.dev/min-scale": "1", "autoscaling.knative.dev/max-scale": "1"}
	})
	if err != nil {
		t.Fatal(err)
	}
	// Register cleanup before waiting for a Pod; failed setup must not leak a Service.
	var pod *corev1.Pod
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if err := clients.ServingClient.Services.Delete(cleanupCtx, svc.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Service: %v", err)
		}
		if pod == nil {
			return
		}
		if err := wait.PollUntilContextCancel(cleanupCtx, time.Second, true, func(ctx context.Context) (bool, error) {
			p, err := clients.KubeClient.CoreV1().Pods(svc.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return err == nil && p.UID != pod.UID, err
		}); err != nil {
			t.Errorf("normal Pod cleanup exceeded timeout: %v", err)
		}
	})
	selector := serving.ServiceLabelKey + "=" + svc.Name
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		pods, err := clients.KubeClient.CoreV1().Pods(svc.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 1 {
			p := &pods.Items[0]
			if len(p.Status.InitContainerStatuses) == 1 && p.Status.InitContainerStatuses[0].State.Running != nil {
				pod = p
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("wait for helper: %v", err)
	}
	if diff := cmp.Diff(helper.StartupProbe, pod.Spec.InitContainers[0].StartupProbe); diff != "" {
		t.Fatalf("Pod startup probe (-want +got): %s", diff)
	}
	if p := pod.Spec.InitContainers[0].RestartPolicy; p == nil || *p != corev1.ContainerRestartPolicyAlways {
		t.Fatal("Pod lost native-sidecar restart policy")
	}
	revisionName := pod.Labels[serving.RevisionLabelKey]
	assertNativeSidecarResources(t, ctx, clients, svc, revisionName, helper)
	if fail {
		err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			p := getTrackedPod(t, ctx, clients, pod)
			assertRegularContainersUnstarted(t, p)
			return p.Status.InitContainerStatuses[0].RestartCount > 0, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		p := getTrackedPod(t, ctx, clients, pod)
		r, err := clients.ServingClient.Revisions.Get(ctx, revisionName, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ds, err := clients.KubeClient.AppsV1().Deployments(svc.Namespace).List(ctx, metav1.ListOptions{LabelSelector: serving.RevisionLabelKey + "=" + revisionName})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Pod conditions=%+v helper=%+v Revision conditions=%+v", p.Status.Conditions, p.Status.InitContainerStatuses, r.Status.Conditions)
		for _, d := range ds.Items {
			t.Logf("Deployment %s status=%+v", d.Name, d.Status)
		}
		return
	}
	base := forwardNativeSidecar(t, ctx, clients, pod)
	request := func(method, path string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s = %d, want %d", method, path, resp.StatusCode, want)
		}
	}
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz/readiness", nil)
		if err != nil {
			return false, err
		}
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			return false, nil // The helper listener may not be accepting connections yet.
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			return false, fmt.Errorf("blocked health status = %d", resp.StatusCode)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("observe blocked startup endpoint: %v", err)
	}
	p := getTrackedPod(t, ctx, clients, pod)
	assertRegularContainersUnstarted(t, p)
	initial := p.Status.InitContainerStatuses[0]
	if initial.Started != nil && *initial.Started {
		t.Fatal("helper already started before release")
	}
	request(http.MethodPost, "/start-succeeding", http.StatusOK)
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		p := getTrackedPod(t, ctx, clients, pod)
		h := p.Status.InitContainerStatuses[0]
		if h.RestartCount != initial.RestartCount {
			t.Fatal("helper restarted during success sequence")
		}
		if h.State.Running == nil || h.Started == nil || !*h.Started || len(p.Status.ContainerStatuses) < 2 {
			return false, nil
		}
		for _, c := range p.Status.ContainerStatuses {
			if c.State.Running == nil {
				return false, nil
			}
		}
		s, err := clients.ServingClient.Services.Get(ctx, svc.Name, metav1.GetOptions{})
		return err == nil && s.IsReady(), err
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := clients.ServingClient.Services.Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pkgTest.CheckEndpointState(ctx, clients.KubeClient, t.Logf, s.Status.URL.URL(), spoof.IsStatusOK,
		"NativeSidecarStartup", test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(ctx, t.Logf, clients, test.ServingFlags.HTTPS)); err != nil {
		t.Fatal(err)
	}
	final := getTrackedPod(t, ctx, clients, pod).Status.InitContainerStatuses[0]
	if final.State.Running == nil || final.RestartCount != initial.RestartCount {
		t.Fatalf("helper did not remain running: %+v", final)
	}
}

func assertRegularContainersUnstarted(t *testing.T, p *corev1.Pod) {
	t.Helper()
	for _, c := range p.Status.ContainerStatuses {
		if c.State.Running != nil || c.State.Terminated != nil || c.LastTerminationState.Terminated != nil || c.ContainerID != "" || c.RestartCount != 0 {
			t.Fatalf("regular container %s started before release: %+v", c.Name, c)
		}
	}
}

func getTrackedPod(t *testing.T, ctx context.Context, clients *test.Clients, original *corev1.Pod) *corev1.Pod {
	t.Helper()
	p, err := clients.KubeClient.CoreV1().Pods(original.Namespace).Get(ctx, original.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if p.UID != original.UID {
		t.Fatal("tracked Pod was replaced during assertions")
	}
	return p
}

func assertNativeSidecarResources(t *testing.T, ctx context.Context, clients *test.Clients, svc *v1.Service, rev string, helper corev1.Container) {
	t.Helper()
	c, err := clients.ServingClient.Configs.Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := clients.ServingClient.Revisions.Get(ctx, rev, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ds, err := clients.KubeClient.AppsV1().Deployments(svc.Namespace).List(ctx, metav1.ListOptions{LabelSelector: serving.RevisionLabelKey + "=" + rev})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Items) != 1 {
		t.Fatalf("get Deployment: count=%d", len(ds.Items))
	}
	for name, cs := range map[string][]corev1.Container{"Configuration": c.Spec.Template.Spec.InitContainers, "Revision": r.Spec.InitContainers, "Deployment": ds.Items[0].Spec.Template.Spec.InitContainers} {
		if len(cs) != 1 || cs[0].RestartPolicy == nil || *cs[0].RestartPolicy != corev1.ContainerRestartPolicyAlways {
			t.Fatalf("%s lost native sidecar", name)
		}
		if diff := cmp.Diff(helper.StartupProbe, cs[0].StartupProbe); diff != "" {
			t.Errorf("%s startup probe (-want +got): %s", name, diff)
		}
	}
}

func forwardNativeSidecar(t *testing.T, ctx context.Context, clients *test.Clients, pod *corev1.Pod) string {
	t.Helper()
	cfg, err := pkgTest.Flags.GetRESTConfig()
	if err != nil {
		t.Fatal(err)
	}
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	u := clients.KubeClient.CoreV1().RESTClient().Post().Resource("pods").Namespace(pod.Namespace).Name(pod.Name).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport, Timeout: 15 * time.Second}, http.MethodPost, u)
	stop, ready := make(chan struct{}), make(chan struct{})
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{"0:9000"}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- forwarder.ForwardPorts()
	}()
	t.Cleanup(func() {
		close(stop)
		select {
		case <-finished:
		case <-time.After(20 * time.Second):
			forwarder.Close()
			t.Error("port-forward session did not close")
		}
	})
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("port-forward ended before ready: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ports, err := forwarder.GetPorts()
	if err != nil || len(ports) != 1 {
		t.Fatalf("get forwarded port: %v, %v", ports, err)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", ports[0].Local)
}
