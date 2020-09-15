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

package resources

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakek8s "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace = "test-namespace"
	testService   = "test-service"
)

func TestScopedEndpointsCounter(t *testing.T) {
	kubeClient := fakek8s.NewSimpleClientset()
	endpointsClient := kubeinformers.NewSharedInformerFactory(kubeClient, 0).Core().V1().Endpoints()
	createEndpoints := func(ep *corev1.Endpoints) {
		kubeClient.CoreV1().Endpoints(testNamespace).Create(context.Background(), ep, metav1.CreateOptions{})
		endpointsClient.Informer().GetIndexer().Add(ep)
	}

	addressCounter := NewScopedEndpointsCounter(endpointsClient.Lister(), testNamespace, testService)

	tests := []struct {
		name         string
		endpoints    *corev1.Endpoints
		wantReady    int
		wantNotReady int
		wantErr      bool
	}{{
		name:         "no endpoints at all",
		endpoints:    nil,
		wantReady:    0,
		wantNotReady: 0,
		wantErr:      true,
	}, {
		name:         "no ready/not-ready addresses",
		endpoints:    endpoints(0, 0),
		wantReady:    0,
		wantNotReady: 0,
	}, {
		name:         "one ready/two not-ready addresses",
		endpoints:    endpoints(1, 2),
		wantReady:    1,
		wantNotReady: 2,
	}, {
		name:         "ten ready/twenty not-ready addresses",
		endpoints:    endpoints(10, 20),
		wantReady:    10,
		wantNotReady: 20,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.endpoints != nil {
				createEndpoints(test.endpoints)
			}
			got, err := addressCounter.ReadyCount()
			if got != test.wantReady {
				t.Errorf("ReadyCount() = %d, wantReady: %d", got, test.wantReady)
			}
			if got, want := (err != nil), test.wantErr; got != want {
				t.Errorf("ReadyCount() wantErr = %v, want: %v, err: %v", got, want, err)
			}

			got, err = addressCounter.NotReadyCount()
			if got != test.wantNotReady {
				t.Errorf("NotReadyCount() = %d, wantNotReady: %d", got, test.wantNotReady)
			}
			if got, want := (err != nil), test.wantErr; got != want {
				t.Errorf("NotReadyCount() wantErr = %v, want: %v, err: %v", got, want, err)
			}
		})
	}
}

func TestReadyAddressCount(t *testing.T) {
	tests := []struct {
		name         string
		endpoints    *corev1.Endpoints
		wantReady    int
		wantNotReady int
	}{{
		name:         "no ready/not-ready addresses",
		endpoints:    endpoints(0, 0),
		wantReady:    0,
		wantNotReady: 0,
	}, {
		name:         "one ready/two not-ready addresses",
		endpoints:    endpoints(1, 2),
		wantReady:    1,
		wantNotReady: 2,
	}, {
		name:         "ten ready/twenty not-ready addresses",
		endpoints:    endpoints(10, 20),
		wantReady:    10,
		wantNotReady: 20,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ReadyAddressCount(test.endpoints); got != test.wantReady {
				t.Errorf("ReadyAddressCount() = %d, want: %d", got, test.wantReady)
			}
			if got := NotReadyAddressCount(test.endpoints); got != test.wantNotReady {
				t.Errorf("NotReadyAddressCount() = %d, want: %d", got, test.wantNotReady)
			}
		})
	}
}

func endpoints(readyIPCount, notReadyIPCount int) *corev1.Endpoints {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testService,
		},
	}
	addresses := make([]corev1.EndpointAddress, readyIPCount)
	notReadyAddresses := make([]corev1.EndpointAddress, notReadyIPCount)

	for i := 0; i < readyIPCount; i++ {
		addresses[i] = corev1.EndpointAddress{IP: fmt.Sprintf("127.0.0.%v", i*3+1)}
	}

	for i := 0; i < notReadyIPCount; i++ {
		notReadyAddresses[i] = corev1.EndpointAddress{IP: fmt.Sprintf("127.0.0.%v", i*3+2)}
	}

	ep.Subsets = []corev1.EndpointSubset{{
		Addresses:         addresses,
		NotReadyAddresses: notReadyAddresses,
	}}
	return ep
}
