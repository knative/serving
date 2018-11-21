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

package clusteringress

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	istiov1alpha3 "github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/config"
	"github.com/knative/serving/pkg/system"
)

var (
	originLabels = map[string]string{"origin-key": "origin-value"}
	newLabels    = map[string]string{"new-key": "new-value"}
)

func TestGatewayUpdateOnConfigChanged(t *testing.T) {
	kubeClient, sharedClient, _, controller, _, kubeInformer, sharedInformer, _, watcher := newTestSetup(t)

	stopCh := make(chan struct{})
	defer func() {
		close(stopCh)
	}()

	kubeInformer.Start(stopCh)
	sharedInformer.Start(stopCh)
	if err := watcher.Start(stopCh); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}

	go controller.Run(1, stopCh)

	gateway := &istiov1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: system.Namespace,
		},
		Spec: istiov1alpha3.GatewaySpec{
			Selector: originLabels,
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: corev1.ServiceSpec{
			Selector: newLabels,
		},
	}

	gatewayClient := sharedClient.NetworkingV1alpha3().Gateways(system.Namespace)
	gatewayWatcher, err := gatewayClient.Watch(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Could not create gateway watcher")
	}
	defer gatewayWatcher.Stop()

	// Create the test gateway.
	gatewayClient.Create(gateway)

	// Create the service for test.
	kubeClient.CoreV1().Services(metav1.NamespaceDefault).Create(service)

	// Test changes in istio config map. Gateway should get updated appropriately.
	tests := []struct {
		name           string
		doThings       func()
		expectedLabels map[string]string
		gatewayExist   bool
		shouldUpdate   bool
	}{{
		name: "invalid url",
		doThings: func() {
			config := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.IstioConfigName,
					Namespace: system.Namespace,
				},
				Data: map[string]string{
					config.IngressGatewayKey: "invalid",
				},
			}
			watcher.OnChange(&config)
		},
		expectedLabels: originLabels,
		gatewayExist:   true,
	}, {
		name: "service not exist",
		doThings: func() {
			config := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.IstioConfigName,
					Namespace: system.Namespace,
				},
				Data: map[string]string{
					config.IngressGatewayKey: "non-exist.ns.svc.cluster.local",
				},
			}
			watcher.OnChange(&config)
		},
		expectedLabels: originLabels,
		gatewayExist:   true,
	}, {
		name: "service not exist",
		doThings: func() {
			config := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.IstioConfigName,
					Namespace: system.Namespace,
				},
				Data: map[string]string{
					config.IngressGatewayKey: "non-exist.ns.svc.cluster.local",
				},
			}
			watcher.OnChange(&config)
		},
		expectedLabels: originLabels,
		gatewayExist:   true,
	}, {
		name: "gateway not exist",
		doThings: func() {
			config := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.IstioConfigName,
					Namespace: system.Namespace,
				},
				Data: map[string]string{
					config.IngressGatewayKey: "test-svc.default.svc.cluster.local",
				},
			}
			watcher.OnChange(&config)
		},
		expectedLabels: originLabels,
		gatewayExist:   false,
	}}

	// TODO(lichuqiang): enable this when client-go dependency is updated
	// to support JSON patch in fake client.
	// See https://github.com/kubernetes/client-go/commit/41406bf985e3fa3c5eccd0a7c1fb39e9212340e8
	/*{
		name: "valid url with suffix",
		doThings: func() {
			config := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.IstioConfigName,
					Namespace: system.Namespace,
				},
				Data: map[string]string{
					config.IngressGatewayKey: "test-svc.default.svc.cluster.local",
				},
			}
			watcher.OnChange(&config)
		},
		expectedLabels: newLabels,
		gatewayExist:   true,
		shouldUpdate:   true,
	}, {
		name: "valid url without suffix",
		doThings: func() {
			config := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.IstioConfigName,
					Namespace: system.Namespace,
				},
				Data: map[string]string{
					config.IngressGatewayKey: "test-svc.default.svc",
				},
			}
			watcher.OnChange(&config)
		},
		expectedLabels: newLabels,
		gatewayExist:   true,
		shouldUpdate:   true,
	}*/
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Init the gateway
			gatewayClient.Delete(gatewayName, &metav1.DeleteOptions{})
			if test.gatewayExist {
				gatewayClient.Create(gateway)
			}
			test.doThings()

			// Record the origin events channel length for comparison later.
			originEvents := len(gatewayWatcher.ResultChan())

			if test.shouldUpdate {
				timer := time.NewTimer(10 * time.Second)
			loop:
				for {
					select {
					case event := <-gatewayWatcher.ResultChan():
						if event.Type == watch.Modified {
							break loop
						}
					case <-timer.C:
						t.Fatalf("gatewayWatcher did not receive a Type==Modified event in 10s")
					}
				}
			} else {
				// TODO(lichuqiang): find more reliable way to make sure that
				// gateway does not get updated.
				time.Sleep(10 * time.Millisecond)
				if len(gatewayWatcher.ResultChan()) > originEvents {
					t.Errorf("Unexpected events on gateway")
				}
			}

			if test.gatewayExist {
				res, err := gatewayClient.Get(gatewayName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Error getting gateway: %v", err)
				}
				if !equality.Semantic.DeepEqual(res.Spec.Selector, test.expectedLabels) {
					t.Errorf("Expected selector %v but saw %v", test.expectedLabels, res.Spec.Selector)
				}
			}
		})
	}
}
