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
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/networking/test/types"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
)

var rootCAs = x509.NewCertPool()

var dialBackoff = wait.Backoff{
	Duration: 50 * time.Millisecond,
	Factor:   1.4,
	Jitter:   0.1, // At most 10% jitter.
	Steps:    100,
	Cap:      10 * time.Second,
}

// uaRoundTripper wraps the given http.RoundTripper and
// sets a custom UserAgent.
type uaRoundTripper struct {
	http.RoundTripper
	ua string
}

// RoundTrip implements http.RoundTripper.
func (ua *uaRoundTripper) RoundTrip(rq *http.Request) (*http.Response, error) {
	rq.Header.Set("User-Agent", ua.ua)
	return ua.RoundTripper.RoundTrip(rq)
}

// CreateRuntimeService creates a Kubernetes service that will respond to the protocol
// specified with the given portName.  It returns the service name, the port on
// which the service is listening, and a "cancel" function to clean up the
// created resources.
func CreateRuntimeService(ctx context.Context, t *testing.T, clients *test.Clients, portName string) (string, int, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Avoid zero, but pick a low port number.
	port := 50 + rand.Intn(50)
	t.Logf("[%s] Using port %d", name, port)

	// Pick a high port number.
	containerPort := 8000 + rand.Intn(100)
	t.Logf("[%s] Using containerPort %d", name, containerPort)

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
				Name:            "foo",
				Image:           pkgTest.ImagePath("runtime"),
				ImagePullPolicy: corev1.PullIfNotPresent,
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
				TargetPort: intstr.FromInt(containerPort),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}

	return name, port, createPodAndService(ctx, t, clients, pod, svc)
}

// CreateProxyService creates a Kubernetes service that will forward requests to
// the specified target.  It returns the service name, the port on which the service
// is listening, and a "cancel" function to clean up the created resources.
func CreateProxyService(ctx context.Context, t *testing.T, clients *test.Clients, target string, gatewayDomain string) (string, int, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Avoid zero, but pick a low port number.
	port := 50 + rand.Intn(50)
	t.Logf("[%s] Using port %d", name, port)

	// Pick a high port number.
	containerPort := 8000 + rand.Intn(100)
	t.Logf("[%s] Using containerPort %d", name, containerPort)

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
				Name:            "foo",
				Image:           pkgTest.ImagePath("httpproxy"),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports: []corev1.ContainerPort{{
					ContainerPort: int32(containerPort),
				}},
				Env: []corev1.EnvVar{{
					Name:  "TARGET_HOST",
					Value: target,
				}, {
					Name:  "PORT",
					Value: strconv.Itoa(containerPort),
				}},
			}},
		},
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
				Port:       int32(port),
				TargetPort: intstr.FromInt(containerPort),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}
	proxyServiceCancel := createPodAndService(ctx, t, clients, pod, svc)

	externalNameServiceCancel := createExternalNameService(ctx, t, clients, target, gatewayDomain)

	return name, port, func() {
		externalNameServiceCancel()
		proxyServiceCancel()
	}
}

// CreateTimeoutService creates a Kubernetes service that will respond to the protocol
// specified with the given portName.  It returns the service name, the port on
// which the service is listening, and a "cancel" function to clean up the
// created resources.
func CreateTimeoutService(ctx context.Context, t *testing.T, clients *test.Clients) (string, int, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Avoid zero, but pick a low port number.
	port := 50 + rand.Intn(50)
	t.Logf("[%s] Using port %d", name, port)

	// Pick a high port number.
	containerPort := 8000 + rand.Intn(100)
	t.Logf("[%s] Using containerPort %d", name, containerPort)

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
				Name:            "foo",
				Image:           pkgTest.ImagePath("timeout"),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports: []corev1.ContainerPort{{
					Name:          networking.ServicePortNameHTTP1,
					ContainerPort: int32(containerPort),
				}},
				// This is needed by the timeout image we are using.
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: strconv.Itoa(containerPort),
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Port: intstr.FromInt(containerPort),
						},
					},
				},
			}},
		},
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
				Name:       networking.ServicePortNameHTTP1,
				Port:       int32(port),
				TargetPort: intstr.FromInt(containerPort),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}

	return name, port, createPodAndService(ctx, t, clients, pod, svc)
}

// CreateFlakyService creates a Kubernetes service where the backing pod will
// succeed only every Nth request.
func CreateFlakyService(ctx context.Context, t *testing.T, clients *test.Clients, period int) (string, int, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Avoid zero, but pick a low port number.
	port := 50 + rand.Intn(50)
	t.Logf("[%s] Using port %d", name, port)

	// Pick a high port number.
	containerPort := 8000 + rand.Intn(100)
	t.Logf("[%s] Using containerPort %d", name, containerPort)

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
				Name:            "foo",
				Image:           pkgTest.ImagePath("flaky"),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports: []corev1.ContainerPort{{
					Name:          networking.ServicePortNameHTTP1,
					ContainerPort: int32(containerPort),
				}},
				// This is needed by the runtime image we are using.
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: strconv.Itoa(containerPort),
				}, {
					Name:  "PERIOD",
					Value: strconv.Itoa(period),
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(containerPort),
						},
					},
				},
			}},
		},
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
				Name:       networking.ServicePortNameHTTP1,
				Port:       int32(port),
				TargetPort: intstr.FromInt(containerPort),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}

	return name, port, createPodAndService(ctx, t, clients, pod, svc)
}

// CreateWebsocketService creates a Kubernetes service that will upgrade the connection
// to use websockets and echo back the received messages with the provided suffix.
func CreateWebsocketService(ctx context.Context, t *testing.T, clients *test.Clients, suffix string) (string, int, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Avoid zero, but pick a low port number.
	port := 50 + rand.Intn(50)
	t.Logf("[%s] Using port %d", name, port)

	// Pick a high port number.
	containerPort := 8000 + rand.Intn(100)
	t.Logf("[%s] Using containerPort %d", name, containerPort)

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
				Name:            "foo",
				Image:           pkgTest.ImagePath("wsserver"),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports: []corev1.ContainerPort{{
					Name:          networking.ServicePortNameHTTP1,
					ContainerPort: int32(containerPort),
				}},
				// This is needed by the runtime image we are using.
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: strconv.Itoa(containerPort),
				}, {
					Name:  "SUFFIX",
					Value: suffix,
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(containerPort),
						},
					},
				},
			}},
		},
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
				Name:       networking.ServicePortNameHTTP1,
				Port:       int32(port),
				TargetPort: intstr.FromInt(containerPort),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}

	return name, port, createPodAndService(ctx, t, clients, pod, svc)
}

// CreateGRPCService creates a Kubernetes service that will upgrade the connection
// to use GRPC and echo back the received messages with the provided suffix.
func CreateGRPCService(ctx context.Context, t *testing.T, clients *test.Clients, suffix string) (string, int, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	// Avoid zero, but pick a low port number.
	port := 50 + rand.Intn(50)
	t.Logf("[%s] Using port %d", name, port)

	// Pick a high port number.
	containerPort := 8000 + rand.Intn(100)
	t.Logf("[%s] Using containerPort %d", name, containerPort)

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
				Name:            "foo",
				Image:           pkgTest.ImagePath("grpc-ping"),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports: []corev1.ContainerPort{{
					Name:          networking.ServicePortNameH2C,
					ContainerPort: int32(containerPort),
				}},
				// This is needed by the runtime image we are using.
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: strconv.Itoa(containerPort),
				}, {
					Name:  "SUFFIX",
					Value: suffix,
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(containerPort),
						},
					},
				},
			}},
		},
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
				Name:       networking.ServicePortNameH2C,
				Port:       int32(port),
				TargetPort: intstr.FromInt(containerPort),
			}},
			Selector: map[string]string{
				"test-pod": name,
			},
		},
	}

	return name, port, createPodAndService(ctx, t, clients, pod, svc)
}

// createService is a helper for creating the service resource.
func createService(ctx context.Context, t *testing.T, clients *test.Clients, svc *corev1.Service) context.CancelFunc {
	t.Helper()

	svcName := ktypes.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}

	t.Cleanup(func() {
		clients.KubeClient.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
	})
	if err := reconciler.RetryTestErrors(func(attempts int) error {
		_, err := clients.KubeClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Error creating Service %q: %v", svcName, err)
	}

	return func() {
		err := clients.KubeClient.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Service %q: %v", svcName, err)
		}
	}
}

func createExternalNameService(ctx context.Context, t *testing.T, clients *test.Clients, target, gatewayDomain string) context.CancelFunc {
	targetName := strings.SplitN(target, ".", 3)
	externalNameSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetName[0],
			Namespace: targetName[1],
		},
		Spec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeExternalName,
			ExternalName:    gatewayDomain,
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports: []corev1.ServicePort{{
				Name:       networking.ServicePortNameH2C,
				Port:       int32(80),
				TargetPort: intstr.FromInt(80),
			}},
		},
	}

	return createService(ctx, t, clients, externalNameSvc)
}

// createPodAndService is a helper for creating the pod and service resources, setting
// up their context.CancelFunc, and waiting for it to become ready.
func createPodAndService(ctx context.Context, t *testing.T, clients *test.Clients, pod *corev1.Pod, svc *corev1.Service) context.CancelFunc {
	t.Helper()

	podName := ktypes.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}
	svcName := ktypes.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}

	t.Cleanup(func() {
		clients.KubeClient.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
	})
	if err := reconciler.RetryTestErrors(func(attempts int) error {
		_, err := clients.KubeClient.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Error creating Pod %q: %v", podName, err)
	}

	t.Cleanup(func() {
		clients.KubeClient.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
	})
	if err := reconciler.RetryTestErrors(func(attempts int) error {
		_, err := clients.KubeClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Error creating Service %q: %v", svcName, err)
	}

	// Wait for the Pod to show up in the Endpoints resource.
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		var ep *corev1.Endpoints
		err := reconciler.RetryTestErrors(func(attempts int) (err error) {
			ep, err = clients.KubeClient.CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
			return err
		})
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
		t.Fatalf("Error waiting for %q Endpoints to contain a Pod IP: %v", svcName, waitErr)
	}

	return func() {
		err := clients.KubeClient.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Service %q: %v", svcName, err)
		}
		err = clients.KubeClient.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Pod %q", pod.Name)
		}
	}
}

// Option enables further configuration of a Ingress.
type Option func(*v1alpha1.Ingress)

// OverrideIngressAnnotation overrides the Ingress annotation.
func OverrideIngressAnnotation(annotations map[string]string) Option {
	return func(ing *v1alpha1.Ingress) {
		ing.Annotations = annotations
	}
}

// createIngress creates a Knative Ingress resource
func createIngress(ctx context.Context, t *testing.T, clients *test.Clients, spec v1alpha1.IngressSpec, io ...Option) (*v1alpha1.Ingress, context.CancelFunc) {
	t.Helper()

	name := test.ObjectNameForTest(t)

	// Create a simple Ingress over the Service.
	ing := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Annotations: map[string]string{
				networking.IngressClassAnnotationKey: test.NetworkingFlags.IngressClass,
			},
		},
		Spec: spec,
	}

	for _, opt := range io {
		opt(ing)
	}

	ingName := ktypes.NamespacedName{Name: ing.Name, Namespace: ing.Namespace}

	ing.SetDefaults(context.Background())
	if err := ing.Validate(context.Background()); err != nil {
		t.Fatalf("Invalid ingress %q: %v", ingName, err)
	}

	t.Cleanup(func() { clients.NetworkingClient.Ingresses.Delete(ctx, ing.Name, metav1.DeleteOptions{}) })
	if err := reconciler.RetryTestErrors(func(attempts int) (err error) {
		ing, err = clients.NetworkingClient.Ingresses.Create(ctx, ing, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Error creating Ingress %q: %v", ingName, err)
	}

	return ing, func() {
		err := clients.NetworkingClient.Ingresses.Delete(ctx, ing.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Ingress %q: %v", ingName, err)
		}
	}
}

func createIngressReadyDialContext(ctx context.Context, t *testing.T, clients *test.Clients, spec v1alpha1.IngressSpec) (*v1alpha1.Ingress, func(context.Context, string, string) (net.Conn, error), context.CancelFunc) {
	t.Helper()

	ing, cancel := createIngress(ctx, t, clients, spec)
	ingName := ktypes.NamespacedName{Name: ing.Name, Namespace: ing.Namespace}

	if err := WaitForIngressState(ctx, clients.NetworkingClient, ing.Name, IsIngressReady, t.Name()); err != nil {
		cancel()
		t.Fatalf("Error waiting for ingress %q state: %v", ingName, err)
	}
	err := reconciler.RetryTestErrors(func(attempts int) (err error) {
		ing, err = clients.NetworkingClient.Ingresses.Get(ctx, ing.Name, metav1.GetOptions{})
		return err
	})
	if err != nil {
		cancel()
		t.Fatalf("Error getting Ingress %q: %v", ingName, err)
	}

	// Create a dialer based on the Ingress' public load balancer.
	return ing, CreateDialContext(ctx, t, ing, clients), cancel
}

func CreateIngressReady(ctx context.Context, t *testing.T, clients *test.Clients, spec v1alpha1.IngressSpec) (*v1alpha1.Ingress, *http.Client, context.CancelFunc) {
	t.Helper()

	// Create a client with a dialer based on the Ingress' public load balancer.
	ing, dialer, cancel := createIngressReadyDialContext(ctx, t, clients, spec)

	// TODO(mattmoor): How to get ing?
	var tlsConfig *tls.Config
	if len(ing.Spec.TLS) > 0 {
		// CAs are added to this as TLS secrets are created.
		tlsConfig = &tls.Config{
			RootCAs: rootCAs,
		}
	}

	return ing, &http.Client{
		Transport: &uaRoundTripper{
			RoundTripper: &http.Transport{
				DialContext:     dialer,
				TLSClientConfig: tlsConfig,
			},
			ua: fmt.Sprintf("knative.dev/%s/%s", t.Name(), ing.Name),
		},
	}, cancel
}

// UpdateIngress updates a Knative Ingress resource
func UpdateIngress(ctx context.Context, t *testing.T, clients *test.Clients, name string, spec v1alpha1.IngressSpec) {
	t.Helper()

	if err := reconciler.RetryTestErrors(func(attempts int) error {
		var ing *v1alpha1.Ingress
		if err := reconciler.RetryTestErrors(func(attempts int) (err error) {
			ing, err = clients.NetworkingClient.Ingresses.Get(ctx, name, metav1.GetOptions{})
			return err
		}); err != nil {
			return err
		}

		ing.Spec = spec

		if err := ing.Validate(context.Background()); err != nil {
			return err
		}

		_, err := clients.NetworkingClient.Ingresses.Update(ctx, ing, metav1.UpdateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Error fetching and updating Ingress %q: %v", name, err)
	}
}

func UpdateIngressReady(ctx context.Context, t *testing.T, clients *test.Clients, name string, spec v1alpha1.IngressSpec) {
	t.Helper()

	UpdateIngress(ctx, t, clients, name, spec)

	if err := WaitForIngressState(ctx, clients.NetworkingClient, name, IsIngressReady, t.Name()); err != nil {
		t.Fatalf("Error waiting for ingress %q state: %v", name, err)
	}
}

// This is based on https://golang.org/src/crypto/tls/generate_cert.go
func CreateTLSSecret(ctx context.Context, t *testing.T, clients *test.Clients, hosts []string) (string, context.CancelFunc) {
	return CreateTLSSecretWithCertPool(ctx, t, clients, hosts, test.ServingNamespace, rootCAs)
}

// CreateTLSSecretWithCertPool creates TLS certificate with given CertPool.
func CreateTLSSecretWithCertPool(ctx context.Context, t *testing.T, clients *test.Clients, hosts []string, ns string, cas *x509.CertPool) (string, context.CancelFunc) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatal("ecdsa.GenerateKey() =", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := cryptorand.Int(cryptorand.Reader, serialNumberLimit)
	if err != nil {
		t.Fatal("Failed to generate serial number:", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Knative Ingress Conformance Testing"},
		},

		// Only let it live briefly.
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(5 * time.Minute),

		IsCA:                  true,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		DNSNames: hosts,
	}

	derBytes, err := x509.CreateCertificate(cryptorand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal("x509.CreateCertificate() =", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatal("ParseCertificate() =", err)
	}
	// Ideally we'd undo this in "cancel", but there doesn't
	// seem to be a mechanism to remove things from a pool.
	cas.AddCert(cert)

	certPEM := &bytes.Buffer{}
	if err := pem.Encode(certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		t.Fatal("Failed to write data to cert.pem:", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal("Unable to marshal private key:", err)
	}
	privPEM := &bytes.Buffer{}
	if err := pem.Encode(privPEM, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		t.Fatal("Failed to write data to key.pem:", err)
	}

	name := test.ObjectNameForTest(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"test-secret": name,
			},
		},
		Type: corev1.SecretTypeTLS,
		StringData: map[string]string{
			corev1.TLSCertKey:       certPEM.String(),
			corev1.TLSPrivateKeyKey: privPEM.String(),
		},
	}
	t.Cleanup(func() {
		clients.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{})
	})
	if _, err := clients.KubeClient.CoreV1().Secrets(secret.Namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating Secret:", err)
	}
	return name, func() {
		err := clients.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Secret %s: %v", secret.Name, err)
		}
	}
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
func CreateDialContext(ctx context.Context, t *testing.T, ing *v1alpha1.Ingress, clients *test.Clients) func(context.Context, string, string) (net.Conn, error) {
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
		t.Fatal("Too few parts in internal domain:", internalDomain)
	}
	name, namespace := parts[0], parts[1]

	var svc *corev1.Service
	err := reconciler.RetryTestErrors(func(attempts int) (err error) {
		svc, err = clients.KubeClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("Unable to retrieve Kubernetes service %s/%s: %v", namespace, name, err)
	}

	dial := network.NewBackoffDialer(dialBackoff)
	if pkgTest.Flags.IngressEndpoint != "" {
		t.Logf("ingressendpoint: %q", pkgTest.Flags.IngressEndpoint)

		// If we're using a manual --ingressendpoint then don't require
		// "type: LoadBalancer", which may not play nice with KinD
		return func(ctx context.Context, _ string, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			for _, sp := range svc.Spec.Ports {
				if fmt.Sprint(sp.Port) == port {
					return dial(ctx, "tcp", fmt.Sprintf("%s:%d", pkgTest.Flags.IngressEndpoint, sp.NodePort))
				}
			}
			return nil, fmt.Errorf("service doesn't contain a matching port: %s", port)
		}
	} else if len(svc.Status.LoadBalancer.Ingress) >= 1 {
		ingress := svc.Status.LoadBalancer.Ingress[0]
		return func(ctx context.Context, _ string, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if ingress.IP != "" {
				return dial(ctx, "tcp", ingress.IP+":"+port)
			}
			if ingress.Hostname != "" {
				return dial(ctx, "tcp", ingress.Hostname+":"+port)
			}
			return nil, errors.New("service ingress does not contain dialing information")
		}
	} else {
		t.Fatal("Service does not have a supported shape (not type LoadBalancer? missing --ingressendpoint?).")
		return nil // Unreachable
	}
}

type RequestOption func(*http.Request)
type ResponseExpectation func(response *http.Response) error

func RuntimeRequest(ctx context.Context, t *testing.T, client *http.Client, url string, opts ...RequestOption) *types.RuntimeInfo {
	return RuntimeRequestWithExpectations(ctx, t, client, url,
		[]ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusOK))},
		false,
		opts...)
}

// RuntimeRequestWithExpectations attempts to make a request to url and return runtime information.
// If connection is successful only then it will validate all response expectations.
// If allowDialError is set to true then function will not fail if connection is a dial error.
func RuntimeRequestWithExpectations(ctx context.Context, t *testing.T, client *http.Client, url string,
	responseExpectations []ResponseExpectation,
	allowDialError bool,
	opts ...RequestOption) *types.RuntimeInfo {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Error("Error creating Request:", err)
		return nil
	}

	for _, opt := range opts {
		opt(req)
	}

	resp, err := client.Do(req)

	if err != nil {
		if !allowDialError || !IsDialError(err) {
			t.Error("Error making GET request:", err)
		}
		return nil
	}

	defer resp.Body.Close()

	for _, e := range responseExpectations {
		if err := e(resp); err != nil {
			t.Error("Error meeting response expectations:", err)
			DumpResponse(ctx, t, resp)
			return nil
		}
	}

	if resp.StatusCode == http.StatusOK {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Error("Unable to read response body:", err)
			DumpResponse(ctx, t, resp)
			return nil
		}
		ri := &types.RuntimeInfo{}
		if err := json.Unmarshal(b, ri); err != nil {
			t.Error("Unable to parse runtime image's response payload:", err)
			return nil
		}
		return ri
	}
	return nil
}

func DumpResponse(ctx context.Context, t *testing.T, resp *http.Response) {
	t.Helper()
	b, err := httputil.DumpResponse(resp, true)
	if err != nil {
		t.Error("Error dumping response:", err)
	}
	t.Log(string(b))
}

func StatusCodeExpectation(statusCodes sets.Int) ResponseExpectation {
	return func(response *http.Response) error {
		if !statusCodes.Has(response.StatusCode) {
			return fmt.Errorf("got unexpected status: %d, expected %v", response.StatusCode, statusCodes)
		}
		return nil
	}
}

func IsDialError(err error) bool {
	if err, ok := err.(*url.Error); ok {
		err, ok := err.Err.(*net.OpError)
		return ok && err.Op == "dial"
	}
	return false
}

// WaitForIngressState polls the status of the Ingress called name from client every
// PollInterval until inState returns `true` indicating it is done, returns an
// error or PollTimeout. desc will be used to name the metric that is emitted to
// track how long it took for name to get into the state checked by inState.
func WaitForIngressState(ctx context.Context, client *test.NetworkingClients, name string, inState func(r *v1alpha1.Ingress) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(ctx, fmt.Sprintf("WaitForIngressState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Ingress
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		err := reconciler.RetryTestErrors(func(attempts int) (err error) {
			lastState, err = client.Ingresses.Get(ctx, name, metav1.GetOptions{})
			return err
		})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("ingress %q is not in desired state, got: %+v: %w", name, lastState, waitErr)
	}
	return nil
}

// IsIngressReady will check the status conditions of the ingress and return true if the ingress is
// ready.
func IsIngressReady(r *v1alpha1.Ingress) (bool, error) {
	return r.IsReady(), nil
}
