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

package activator

import (
	"errors"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"knative.dev/pkg/controller"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	servingfake "knative.dev/serving/pkg/client/clientset/versioned/fake"
	servinginformers "knative.dev/serving/pkg/client/informers/externalversions"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
)

func revision(revID types.NamespacedName, protocol networking.ProtocolType) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revID.Namespace,
			Name:      revID.Name,
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Ports: []corev1.ContainerPort{{
							Name: string(protocol),
						}},
					}},
				},
			},
		},
	}
}

func privateSksService(revID types.NamespacedName, clusterIP string, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revID.Namespace,
			Name:      revID.Name,
			Labels: map[string]string{
				serving.RevisionLabelKey:  revID.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: clusterIP,
			Ports:     ports,
		},
	}
}

func TestRevisionWatcher(t *testing.T) {
	for _, tc := range []struct {
		name               string
		dests              []string
		protocol           networking.ProtocolType
		clusterPort        corev1.ServicePort
		clusterIP          string
		expectUpdates      []RevisionDestsUpdate
		probeHostResponses map[string][]activatortest.FakeResponse
		probeResponses     []activatortest.FakeResponse
		ticks              []time.Time
		updateCnt          int
	}{{
		name:  "single healthy podIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{{Dests: []string{"128.0.0.1:1234"}}},
		probeResponses: []activatortest.FakeResponse{{
			Err: errors.New("clusterIP transport error"),
		}, {
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
	}, {
		name:     "single http2 podIP",
		dests:    []string{"128.0.0.1:1234"},
		protocol: networking.ProtocolH2C,
		clusterPort: corev1.ServicePort{
			Name: "http2",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{{Dests: []string{"128.0.0.1:1234"}}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:     "single http2 clusterIP",
		dests:    []string{"128.0.0.1:1234"},
		protocol: networking.ProtocolH2C,
		clusterPort: corev1.ServicePort{
			Name: "http2",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{{ClusterIPDest: "129.0.0.1:1234"}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
		},
	}, {
		name:  "single unavailable podIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{{Dests: []string{}}},
		probeResponses: []activatortest.FakeResponse{{
			Err:  nil,
			Code: http.StatusServiceUnavailable,
			Body: queue.Name,
		}},
	}, {
		name:  "single error podIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{{Dests: []string{}}},
		probeResponses: []activatortest.FakeResponse{{
			Err:  errors.New("Fake error"),
			Code: http.StatusOK,
			Body: queue.Name,
		}},
	}, {
		name:  "podIP slow ready",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{{Dests: []string{}}, {Dests: []string{"128.0.0.1:1234"}}},
		probeResponses: []activatortest.FakeResponse{{
			Err: errors.New("clusterIP transport error"),
		}, {
			Err:  nil,
			Code: http.StatusServiceUnavailable,
			Body: queue.Name,
		}, {
			Err: errors.New("clusterIP transport error"),
		}, {
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
		ticks:     []time.Time{time.Now()},
		updateCnt: 2,
	}, {
		name:  "multiple healthy podIP",
		dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{
			{Dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"}},
		},
		probeResponses: []activatortest.FakeResponse{{
			Err: errors.New("clusterIP transport error"),
		}, {
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
	}, {
		name:  "one healthy one unhealthy podIP",
		dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{
			{Dests: []string{"128.0.0.2:1234"}},
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.2:1234": {{
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "podIP slow ready then clusterIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []RevisionDestsUpdate{
			{Dests: []string{}},
			{Dests: []string{"128.0.0.1:1234"}},
			{ClusterIPDest: "129.0.0.1:1234"},
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Err:  nil,
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}, {
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		ticks:     []time.Time{time.Now(), time.Now()},
		updateCnt: 3,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			defer ClearAll()
			fakeRt := activatortest.FakeRoundTripper{
				ExpectHost:         "test-revision",
				ProbeHostResponses: tc.probeHostResponses,
				ProbeResponses:     tc.probeResponses,
			}
			rt := network.RoundTripperFunc(fakeRt.RT)

			updateCh := make(chan *RevisionDestsUpdate, len(tc.ticks)+1)
			tickerCh := make(chan time.Time)
			defer close(tickerCh)

			// This gets cleaned up as part of the test
			destsCh := make(chan []string)

			// Default for updateCnt is 1
			if tc.updateCnt == 0 {
				tc.updateCnt = 1
			}

			// Default for protocol is http1
			if tc.protocol == "" {
				tc.protocol = networking.ProtocolHTTP1
			}

			fake := kubefake.NewSimpleClientset()
			informer := kubeinformers.NewSharedInformerFactory(fake, 0)
			servicesLister := informer.Core().V1().Services().Lister()

			revID := types.NamespacedName{Namespace: "test-namespace", Name: "test-revision"}
			if tc.clusterIP != "" {
				svc := privateSksService(revID, tc.clusterIP, []corev1.ServicePort{tc.clusterPort})
				fake.Core().Services(svc.Namespace).Create(svc)
				informer.Core().V1().Services().Informer().GetIndexer().Add(svc)
			}

			rw := newRevisionWatcher(
				revID,
				tc.protocol,
				updateCh,
				destsCh,
				rt,
				servicesLister,
				TestLogger(t),
			)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				rw.runWithTickCh(tickerCh)
			}()

			destsCh <- tc.dests

			for _, tick := range tc.ticks {
				tickerCh <- tick
			}

			updates := []RevisionDestsUpdate{}
			for i := 0; i < tc.updateCnt; i++ {
				select {
				case update := <-updateCh:
					sort.Strings(update.Dests)
					updates = append(updates, *update)
				case <-time.After(200 * time.Millisecond):
					t.Errorf("Timed out waiting for update event")
				}
			}

			// Shutdown run loop.
			close(destsCh)

			wg.Wait()

			// Autofill out Rev in expectUpdates
			for i := range tc.expectUpdates {
				tc.expectUpdates[i].Rev = revID
			}

			if got, want := tc.expectUpdates, updates; !cmp.Equal(got, want) {
				t.Errorf("revisionDests updates = %v, want: %v, diff (-want, +got):\n %s", got, want, cmp.Diff(want, got))
			}
		})
	}
}

func ep(revL string, port int32, portName string, ips ...string) *corev1.Endpoints {
	ss := corev1.EndpointSubset{
		Ports: []corev1.EndpointPort{{
			Name: portName,
			Port: port,
		}},
	}
	for _, ip := range ips {
		ss.Addresses = append(ss.Addresses, corev1.EndpointAddress{IP: ip})
	}
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: revL + "-ep",
			Labels: map[string]string{
				serving.RevisionUID:       time.Now().Format("150415.000"),
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
				serving.RevisionLabelKey:  revL,
			},
		},
		Subsets: []corev1.EndpointSubset{ss},
	}
}

func TestRevisionBackendManagerAddEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name               string
		endpointsArr       []*corev1.Endpoints
		revisions          []*v1alpha1.Revision
		services           []*corev1.Service
		probeResponses     []activatortest.FakeResponse
		probeHostResponses map[string][]activatortest.FakeResponse
		expectDests        map[types.NamespacedName]RevisionDestsUpdate
		updateCnt          int
	}{{
		name:         "Add slow healthy",
		endpointsArr: []*corev1.Endpoints{ep("test-revision", 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revision(types.NamespacedName{"test-namespace", "test-revision"}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSksService(types.NamespacedName{"test-namespace", "test-revision"}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Err:  nil,
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}, {
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]RevisionDestsUpdate{
			{Namespace: "test-namespace", Name: "test-revision"}: {
				Dests: []string{"128.0.0.1:1234"},
			},
		},
		updateCnt: 2,
	}, {
		name:         "Add slow ready http2",
		endpointsArr: []*corev1.Endpoints{ep("test-revision", 1234, "http2", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revision(types.NamespacedName{"test-namespace", "test-revision"}, networking.ProtocolH2C),
		},
		services: []*corev1.Service{
			privateSksService(types.NamespacedName{"test-namespace", "test-revision"}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http2", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Err:  nil,
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}, {
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]RevisionDestsUpdate{
			{Namespace: "test-namespace", Name: "test-revision"}: {
				Dests: []string{"128.0.0.1:1234"},
			},
		},
		updateCnt: 2,
	}, {
		name: "Multiple revisions",
		endpointsArr: []*corev1.Endpoints{
			ep("test-revision1", 1234, "http", "128.0.0.1"),
			ep("test-revision2", 1235, "http", "128.1.0.2"),
		},
		revisions: []*v1alpha1.Revision{
			revision(types.NamespacedName{"test-namespace", "test-revision1"}, networking.ProtocolHTTP1),
			revision(types.NamespacedName{"test-namespace", "test-revision2"}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSksService(types.NamespacedName{"test-namespace", "test-revision1"}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 2345}}),
			privateSksService(types.NamespacedName{"test-namespace", "test-revision2"}, "129.0.0.2",
				[]corev1.ServicePort{{Name: "http", Port: 2345}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:2345": {{Err: errors.New("clusterIP transport error")}},
			"129.0.0.2:2345": {{Err: errors.New("clusterIP transport error")}},
		},
		expectDests: map[types.NamespacedName]RevisionDestsUpdate{
			{Namespace: "test-namespace", Name: "test-revision1"}: {
				Dests: []string{"128.0.0.1:1234"},
			},
			{Namespace: "test-namespace", Name: "test-revision2"}: {
				Dests: []string{"128.1.0.2:1235"},
			},
		},
		updateCnt: 2,
	}, {
		name:         "slow podIP then clusterIP",
		endpointsArr: []*corev1.Endpoints{ep("test-revision", 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revision(types.NamespacedName{"test-namespace", "test-revision"}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSksService(types.NamespacedName{"test-namespace", "test-revision"}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Err:  nil,
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}, {
				Err:  nil,
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]RevisionDestsUpdate{
			{Namespace: "test-namespace", Name: "test-revision"}: {
				ClusterIPDest: "129.0.0.1:1234",
			},
		},
		updateCnt: 3,
	}} {

		t.Run(tc.name, func(t *testing.T) {
			defer ClearAll()
			fakeRt := activatortest.FakeRoundTripper{
				ExpectHost:         "test-revision",
				ProbeHostResponses: tc.probeHostResponses,
				ProbeResponses:     tc.probeResponses,
			}
			rt := network.RoundTripperFunc(fakeRt.RT)

			fake := kubefake.NewSimpleClientset()
			informer := kubeinformers.NewSharedInformerFactory(fake, 0)
			endpointsInformer := informer.Core().V1().Endpoints()
			servicesLister := informer.Core().V1().Services().Lister()

			servfake := servingfake.NewSimpleClientset()
			servinginformer := servinginformers.NewSharedInformerFactory(servfake, 0)
			revisions := servinginformer.Serving().V1alpha1().Revisions()
			revisionLister := revisions.Lister()

			// Add the revision we're testing.
			for _, rev := range tc.revisions {
				servfake.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
				revisions.Informer().GetIndexer().Add(rev)
			}

			for _, svc := range tc.services {
				fake.Core().Services(svc.Namespace).Create(svc)
				informer.Core().V1().Services().Informer().GetIndexer().Add(svc)
			}

			stopCh := make(chan struct{})
			defer close(stopCh)
			controller.StartInformers(stopCh, endpointsInformer.Informer())

			updateCh := make(chan *RevisionDestsUpdate, 100)
			bm := NewRevisionBackendsManagerWithProbeFrequency(updateCh, rt, revisionLister,
				servicesLister, endpointsInformer, TestLogger(t), 50*time.Millisecond)
			defer bm.Clear()

			for _, ep := range tc.endpointsArr {
				fake.CoreV1().Endpoints("test-namespace").Create(ep)
				endpointsInformer.Informer().GetIndexer().Add(ep)
			}

			if tc.updateCnt == 0 {
				tc.updateCnt = 1
			}

			revDests := make(map[types.NamespacedName]RevisionDestsUpdate)
			// Wait for updateCb to be called
			for i := 0; i < tc.updateCnt; i++ {
				select {
				case update := <-updateCh:
					sort.Strings(update.Dests)
					revDests[update.Rev] = *update
				case <-time.After(300 * time.Millisecond):
					t.Errorf("Timed out waiting for update event")
				}
			}

			// Update expectDests so we dont have to write out Rev for each test case
			for rev, destUpdate := range tc.expectDests {
				destUpdate.Rev = rev
				tc.expectDests[rev] = destUpdate
			}

			if got, want := revDests, tc.expectDests; !cmp.Equal(got, want) {
				t.Errorf("RevisionDests = %v, want: %v, diff(-want,+got):%s\n", got, want, cmp.Diff(want, got))
			}
		})
	}
}
