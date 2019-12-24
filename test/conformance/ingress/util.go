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

package ingress

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

// CreateService creates a Kubernetes service that will respond to the protocol
// specified with the given portName.  It returns the service name, the port on
// which the service is listening, and a "cancel" function to clean up the
// created resources.
func CreateService(t *testing.T, clients *test.Clients, portName string) (string, int, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Avoid zero, but pick a low port number.
	port := 3 + rand.Intn(97)
	t.Logf("Using port %d", port)

	// Pick a high port number.
	containerPort := 8000 + rand.Intn(100)
	t.Logf("Using containerPort %d", containerPort)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Labels: map[string]string{
				"test-pod": name,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "foo",
				Image: pkgTest.ImagePath("runtime"),
				Ports: []corev1.ContainerPort{{
					Name:          portName,
					ContainerPort: int32(containerPort),
				}},
				// This is needed by the runtime image we are using.
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: strconv.Itoa(containerPort),
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt(containerPort),
						},
					},
				},
			}},
		},
	}
	test.CleanupOnInterrupt(func() { clients.KubeClient.Kube.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &metav1.DeleteOptions{}) })
	pod, err := clients.KubeClient.Kube.CoreV1().Pods(pod.Namespace).Create(pod)
	if err != nil {
		t.Fatalf("Error creating Pod: %v", err)
	}
	cancel := func() {
		err := clients.KubeClient.Kube.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Pod %s", pod.Name)
		}
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Labels: map[string]string{
				"test-pod": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: "ClusterIP",
			Ports: []corev1.ServicePort{{
				Name:       portName,
				Port:       int32(port),
				TargetPort: intstr.FromInt(int(containerPort)),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}
	test.CleanupOnInterrupt(func() {
		clients.KubeClient.Kube.CoreV1().Services(svc.Namespace).Delete(svc.Name, &metav1.DeleteOptions{})
	})
	svc, err = clients.KubeClient.Kube.CoreV1().Services(svc.Namespace).Create(svc)
	if err != nil {
		cancel()
		t.Fatalf("Error creating Service: %v", err)
	}

	// Wait for the Pod to show up in the Endpoints resource.
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		ep, err := clients.KubeClient.Kube.CoreV1().Endpoints(svc.Namespace).Get(svc.Name, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return true, err
		}
		for _, subset := range ep.Subsets {
			if len(subset.Addresses) == 0 {
				return false, nil
			}
		}
		return len(ep.Subsets) > 0, nil
	})
	if waitErr != nil {
		cancel()
		t.Fatalf("Error waiting for Endpoints to contain a Pod IP: %v", waitErr)
	}

	return name, port, func() {
		err := clients.KubeClient.Kube.CoreV1().Services(svc.Namespace).Delete(svc.Name, &metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Service %s", svc.Name)
		}
		cancel()
	}
}

// CreateIngress creates a Knative Ingress resource
func CreateIngress(t *testing.T, clients *test.Clients, spec v1alpha1.IngressSpec) (string, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Create a simple Ingress over the Service.
	ing := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Annotations: map[string]string{
				networking.IngressClassAnnotationKey: test.ServingFlags.IngressClass,
			},
		},
		Spec: spec,
	}
	test.CleanupOnInterrupt(func() { clients.NetworkingClient.Ingresses.Delete(ing.Name, &metav1.DeleteOptions{}) })
	ing, err := clients.NetworkingClient.Ingresses.Create(ing)
	if err != nil {
		t.Fatalf("Error creating Ingress: %v", err)
	}

	return name, func() {
		err := clients.NetworkingClient.Ingresses.Delete(ing.Name, &metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Ingress %s", ing.Name)
		}
	}
}

func CreateIngressReady(t *testing.T, clients *test.Clients, spec v1alpha1.IngressSpec) (string, *http.Client, context.CancelFunc) {
	t.Helper()
	name, cancel := CreateIngress(t, clients, spec)

	if err := v1a1test.WaitForIngressState(clients.NetworkingClient, name, v1a1test.IsIngressReady, t.Name()); err != nil {
		cancel()
		t.Fatalf("Error waiting for ingress state: %v", err)
	}
	ing, err := clients.NetworkingClient.Ingresses.Get(name, metav1.GetOptions{})
	if err != nil {
		cancel()
		t.Fatalf("Error getting Ingress: %v", err)
	}

	// Create a client with a dialer based on the Ingress' public load balancer.
	return name, &http.Client{
		Transport: &http.Transport{
			DialContext: CreateDialContext(t, ing, clients),
		},
	}, cancel
}

// CreateDialContext looks up the endpoint information to create a "dialer" for
// the provided Ingress' public ingress loas balancer.  It can be used to
// contact external-visibility services with an HTTP client via:
//
//	client := &http.Client{
//		Transport: &http.Transport{
//			DialContext: CreateDialContext(t, ing, clients),
//		},
//	}
func CreateDialContext(t *testing.T, ing *v1alpha1.Ingress, clients *test.Clients) func(context.Context, string, string) (net.Conn, error) {
	t.Helper()
	if ing.Status.PublicLoadBalancer == nil || len(ing.Status.PublicLoadBalancer.Ingress) < 1 {
		t.Fatal("Ingress does not have a public load balancer assigned.")
	}

	// TODO(mattmoor): I'm open to tricks that would let us cleanly test multiple
	// public load balancers or LBs with multiple ingresses (below), but want to
	// keep our simple tests simple, thus the [0]s...

	// We expect an ingress LB with the form foo.bar.svc.cluster.local (though
	// we aren't strictly sensitive to the suffix, this is just illustrative.
	internalDomain := ing.Status.PublicLoadBalancer.Ingress[0].DomainInternal
	parts := strings.SplitN(internalDomain, ".", 3)
	if len(parts) < 3 {
		t.Fatalf("Too few parts in internal domain: %s", internalDomain)
	}
	name, namespace := parts[0], parts[1]

	svc, err := clients.KubeClient.Kube.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unable to retrieve Kubernetes service %s/%s: %v", namespace, name, err)
	}
	if len(svc.Status.LoadBalancer.Ingress) < 1 {
		t.Fatal("Service does not have any ingresses (not type LoadBalancer?).")
	}
	ingress := svc.Status.LoadBalancer.Ingress[0]

	return func(_ context.Context, _ string, address string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if ingress.IP != "" {
			return net.Dial("tcp", ingress.IP+":"+port)
		}
		if ingress.Hostname != "" {
			return net.Dial("tcp", ingress.Hostname+":"+port)
		}
		return nil, errors.New("Service ingress does not contain dialing information.")
	}
}
