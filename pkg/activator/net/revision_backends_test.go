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

package net

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	"knative.dev/serving/pkg/queue"

	. "knative.dev/pkg/logging/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

// revisionCC1 - creates a revision with concurrency == 1.
func revisionCC1(revID types.NamespacedName, protocol networking.ProtocolType) *v1alpha1.Revision {
	return revision(revID, protocol, 1)
}

func revision(revID types.NamespacedName, protocol networking.ProtocolType, cc int64) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revID.Namespace,
			Name:      revID.Name,
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(cc),
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

func privateSKSService(revID types.NamespacedName, clusterIP string, ports []corev1.ServicePort) *corev1.Service {
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

func waitForRevisionBackedMananger(t *testing.T, rbm *revisionBackendsManager) {
	timeout := time.After(200 * time.Millisecond)
	for {
		select {
		// rbm.updates() gets closed after all revisionWatchers have finished
		case _, ok := <-rbm.updates():
			if !ok {
				return
			}
		case <-timeout:
			t.Error("Timed out waiting for revisionBackendManager to finish")
			return
		}
	}
}

func TestRevisionWatcher(t *testing.T) {
	logger := TestLogger(t)
	for _, tc := range []struct {
		name                  string
		dests                 []string
		protocol              networking.ProtocolType
		clusterPort           corev1.ServicePort
		clusterIP             string
		expectUpdates         []revisionDestsUpdate
		probeHostResponses    map[string][]activatortest.FakeResponse
		initialClusterIPState bool
		noPodAddressability   bool // This keeps the test defs shorter.
	}{{
		name:  "single healthy podIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.1:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:     "single http2 podIP",
		dests:    []string{"128.0.0.1:1234"},
		protocol: networking.ProtocolH2C,
		clusterPort: corev1.ServicePort{
			Name: "http2",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.1:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
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
		clusterIP:           "129.0.0.1",
		noPodAddressability: true,
		expectUpdates:       []revisionDestsUpdate{{ClusterIPDest: "129.0.0.1:1234", Dests: sets.NewString("128.0.0.1:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
		},
	}, {
		name:      "no pods",
		dests:     []string{},
		clusterIP: "129.0.0.1",
	}, {
		name:                  "no pods, was happy",
		dests:                 []string{},
		clusterIP:             "129.0.0.1",
		initialClusterIPState: true,
	}, {
		name:  "single unavailable podIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP: "129.0.0.1",
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
			}},
		},
	}, {
		name:  "single error podIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP: "129.0.0.1",
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("Fake error"),
			}},
		},
	}, {
		name:  "podIP slow ready",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.1:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "multiple healthy podIP",
		dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.2:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "one healthy one unhealthy podIP",
		dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.2:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.2:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "one healthy one unhealthy podIP then both healthy",
		dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 4321,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{
			{Dests: sets.NewString("128.0.0.2:1234")},
			{Dests: sets.NewString("128.0.0.2:1234", "128.0.0.1:1234")},
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.2:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "clusterIP slow ready, no pod addressability",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234"),
		}},
		noPodAddressability: true,
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("podIP transport error"),
			}, {
				Err: errors.New("podIP transport error"),
			}},
		},
	}, {
		name:  "clusterIP  ready, no pod addressability",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1235,
		},
		noPodAddressability: true,
		clusterIP:           "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{
			ClusterIPDest: "129.0.0.1:1235",
			Dests:         sets.NewString("128.0.0.1:1234"),
		}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("podIP transport error"),
			}},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			fakeRT := activatortest.FakeRoundTripper{
				ExpectHost:         testRevision,
				ProbeHostResponses: tc.probeHostResponses,
			}
			rt := network.RoundTripperFunc(fakeRT.RT)

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

			updateCh := make(chan revisionDestsUpdate, len(tc.expectUpdates)+1)
			defer close(updateCh)

			// This gets closed up by revisionWatcher
			destsCh := make(chan sets.String)

			// Default for protocol is http1
			if tc.protocol == "" {
				tc.protocol = networking.ProtocolHTTP1
			}

			fake := fakekubeclient.Get(ctx)
			informer := fakeserviceinformer.Get(ctx)

			revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
			if tc.clusterIP != "" {
				svc := privateSKSService(revID, tc.clusterIP, []corev1.ServicePort{tc.clusterPort})
				fake.CoreV1().Services(svc.Namespace).Create(svc)
				informer.Informer().GetIndexer().Add(svc)
			}

			waitInformers, err := controller.RunInformers(ctx.Done(), informer.Informer())
			if err != nil {
				t.Fatalf("Failed to start informers: %v", err)
			}
			defer func() {
				cancel()
				waitInformers()
			}()

			rw := newRevisionWatcher(
				ctx,
				revID,
				tc.protocol,
				updateCh,
				destsCh,
				rt,
				informer.Lister(),
				logger,
			)
			rw.clusterIPHealthy = tc.initialClusterIPState

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				rw.run(100 * time.Millisecond)
			}()

			destsCh <- sets.NewString(tc.dests...)

			updates := []revisionDestsUpdate{}
			for i := 0; i < len(tc.expectUpdates); i++ {
				select {
				case update := <-updateCh:
					updates = append(updates, update)
				case <-time.After(200 * time.Millisecond):
					t.Error("Timed out waiting for update event")
				}
			}
			if got, want := rw.podsAddressable, !tc.noPodAddressability; got != want {
				t.Errorf("Revision pod addressability = %v, want: %v", got, want)
			}

			// Shutdown run loop.
			cancel()

			wg.Wait()
			assertChClosed(t, rw.done)

			// Autofill out Rev in expectUpdates
			for i := range tc.expectUpdates {
				tc.expectUpdates[i].Rev = revID
			}

			if got, want := updates, tc.expectUpdates; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Errorf("revisionDests updates = %v, want: %v, diff (-want, +got):\n %s", got, want, cmp.Diff(want, got))
			}
		})
	}
}

func assertChClosed(t *testing.T, ch chan struct{}) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("the channel was not closed")
		}
	}()
	select {
	case ch <- struct{}{}:
		// Panics if the channel is closed
	default:
		// Prevents from blocking forever if the channel is not closed
	}
}

func epSubset(port int32, portName string, ips []string) *corev1.EndpointSubset {
	ss := &corev1.EndpointSubset{
		Ports: []corev1.EndpointPort{{
			Name: portName,
			Port: port,
		}},
	}
	for _, ip := range ips {
		ss.Addresses = append(ss.Addresses, corev1.EndpointAddress{IP: ip})
	}
	return ss
}

func ep(revL string, port int32, portName string, ips ...string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: revL + "-ep",
			Labels: map[string]string{
				serving.RevisionUID:       time.Now().Format("150415.000"),
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
				serving.RevisionLabelKey:  revL,
			},
		},
		Subsets: []corev1.EndpointSubset{*epSubset(port, portName, ips)},
	}
}

func TestRevisionBackendManagerAddEndpoint(t *testing.T) {
	// Make sure we wait out all the jitter in the system.
	for _, tc := range []struct {
		name               string
		endpointsArr       []*corev1.Endpoints
		revisions          []*v1alpha1.Revision
		services           []*corev1.Service
		probeHostResponses map[string][]activatortest.FakeResponse
		expectDests        map[types.NamespacedName]revisionDestsUpdate
		updateCnt          int
	}{{
		name:         "add slow healthy",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{testNamespace, testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: testRevision}: {
				Dests: sets.NewString("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name:         "add slow ready http2",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http2", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolH2C),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{testNamespace, testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http2", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: testRevision}: {
				Dests: sets.NewString("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name: "multiple revisions",
		endpointsArr: []*corev1.Endpoints{
			ep("test-revision1", 1234, "http", "128.0.0.1"),
			ep("test-revision2", 1235, "http", "128.1.0.2"),
		},
		revisions: []*v1alpha1.Revision{
			revisionCC1(types.NamespacedName{testNamespace, "test-revision1"}, networking.ProtocolHTTP1),
			revisionCC1(types.NamespacedName{testNamespace, "test-revision2"}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{testNamespace, "test-revision1"}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 2345}}),
			privateSKSService(types.NamespacedName{testNamespace, "test-revision2"}, "129.0.0.2",
				[]corev1.ServicePort{{Name: "http", Port: 2345}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:2345": {{Err: errors.New("clusterIP transport error")}},
			"129.0.0.2:2345": {{Err: errors.New("clusterIP transport error")}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: "test-revision1"}: {
				Dests: sets.NewString("128.0.0.1:1234"),
			},
			{Namespace: testNamespace, Name: "test-revision2"}: {
				Dests: sets.NewString("128.1.0.2:1235"),
			},
		},
		updateCnt: 2,
	}, {
		name:         "slow podIP",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{testNamespace, testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: testRevision}: {
				Dests: sets.NewString("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name:         "no pod addressability",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{testNamespace, testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: testRevision}: {
				ClusterIPDest: "129.0.0.1:1234",
				Dests:         sets.NewString("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name:         "unhealthy",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{testNamespace, testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			fakeRT := activatortest.FakeRoundTripper{
				ExpectHost:         testRevision,
				ProbeHostResponses: tc.probeHostResponses,
			}
			rt := network.RoundTripperFunc(fakeRT.RT)

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

			endpointsInformer := fakeendpointsinformer.Get(ctx)
			serviceInformer := fakeserviceinformer.Get(ctx)
			revisions := fakerevisioninformer.Get(ctx)

			// Add the revision we're testing.
			for _, rev := range tc.revisions {
				fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
				revisions.Informer().GetIndexer().Add(rev)
			}

			for _, svc := range tc.services {
				fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(svc)
				serviceInformer.Informer().GetIndexer().Add(svc)
			}

			waitInformers, err := controller.RunInformers(ctx.Done(), endpointsInformer.Informer())
			if err != nil {
				t.Fatalf("Failed to start informers: %v", err)
			}
			defer func() {
				cancel()
				waitInformers()
			}()

			rbm := newRevisionBackendsManagerWithProbeFrequency(ctx, rt, 50*time.Millisecond)

			for _, ep := range tc.endpointsArr {
				fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ep)
				endpointsInformer.Informer().GetIndexer().Add(ep)
			}

			revDests := make(map[types.NamespacedName]revisionDestsUpdate)
			// Wait for updateCb to be called
			for i := 0; i < tc.updateCnt; i++ {
				select {
				case update := <-rbm.updates():
					revDests[update.Rev] = update
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

			cancel()
			waitForRevisionBackedMananger(t, rbm)
		})
	}
}

func TestCheckDests(t *testing.T) {
	// This test covers some edge cases in `checkDests` which are next to impossible to
	// test via tests above.

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	svc := privateSKSService(
		types.NamespacedName{testNamespace, testRevision},
		"129.0.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(svc)
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

	waitInformers, err := controller.RunInformers(ctx.Done(), si.Informer())
	if err != nil {
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Make it buffered,so that we can make the test linear.
	uCh := make(chan revisionDestsUpdate, 1)
	dCh := make(chan struct{})
	rw := &revisionWatcher{
		clusterIPHealthy: true,
		podsAddressable:  false,
		rev:              types.NamespacedName{testNamespace, testRevision},
		updateCh:         uCh,
		serviceLister:    si.Lister(),
		logger:           TestLogger(t),
		stopCh:           dCh,
	}
	rw.checkDests(sets.NewString("10.1.1.5"))
	select {
	case <-uCh:
		// Success.
	default:
		t.Error("Expected update but it never went out.")
	}

	close(dCh)
	rw.checkDests(sets.NewString("10.1.1.5"))
	select {
	case <-uCh:
		t.Error("Expected no update but got one")
	default:
		// Success.
	}
}

func TestCheckDestsSwinging(t *testing.T) {
	// This test permits us to test the case when endpoints actually change
	// underneath (e.g. pod crash/restart).
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	svc := privateSKSService(
		types.NamespacedName{testNamespace, testRevision},
		"10.5.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)

	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(svc)
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

	waitInformers, err := controller.RunInformers(ctx.Done(), si.Informer())
	if err != nil {
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	fakeRT := activatortest.FakeRoundTripper{
		ExpectHost: testRevision,
		ProbeHostResponses: map[string][]activatortest.FakeResponse{
			"10.5.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}},
			"10.0.0.1:1234": {{
				Err: errors.New("podIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"10.0.0.2:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}, {
				Err: errors.New("podIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"10.0.0.3:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}

	// Make it buffered,so that we can make the test linear.
	uCh := make(chan revisionDestsUpdate, 1)
	dCh := make(chan struct{})
	rw := &revisionWatcher{
		rev:             types.NamespacedName{testNamespace, testRevision},
		updateCh:        uCh,
		serviceLister:   si.Lister(),
		logger:          TestLogger(t),
		stopCh:          dCh,
		podsAddressable: true,
		transport:       network.RoundTripperFunc(fakeRT.RT),
	}
	// First not ready, second good, clusterIP: not ready.
	rw.checkDests(sets.NewString("10.0.0.1:1234", "10.0.0.2:1234"))
	want := revisionDestsUpdate{
		Rev:           types.NamespacedName{testNamespace, testRevision},
		ClusterIPDest: "",
		Dests:         sets.NewString("10.0.0.2:1234"),
	}

	select {
	case got := <-uCh:
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Second gone, first becomes ready, clusterIP still not ready.
	rw.checkDests(sets.NewString("10.0.0.1:1234"))
	select {
	case got := <-uCh:
		want.Dests = sets.NewString("10.0.0.1:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Second is back, but not healthy yet.
	rw.checkDests(sets.NewString("10.0.0.1:1234", "10.0.0.2:1234"))
	select {
	case got := <-uCh:
		// No update should be sent out, since there's only healthy pod, same as above.
		t.Errorf("Got = %#v, expected no update", got)
	default:
	}

	// All pods are happy now.
	rw.checkDests(sets.NewString("10.0.0.1:1234", "10.0.0.2:1234"))
	select {
	case got := <-uCh:
		want.Dests = sets.NewString("10.0.0.2:1234", "10.0.0.1:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Make sure we do not send out redundant updates.
	rw.checkDests(sets.NewString("10.0.0.1:1234", "10.0.0.2:1234"))
	select {
	case got := <-uCh:
		t.Errorf("Expected no update, but got %#v", got)
	default:
		// Success.
	}

	// Swing to a different pods.
	rw.checkDests(sets.NewString("10.0.0.3:1234", "10.0.0.2:1234"))
	select {
	case got := <-uCh:
		want.Dests = sets.NewString("10.0.0.2:1234", "10.0.0.3:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Scale down by 1.
	rw.checkDests(sets.NewString("10.0.0.2:1234"))
	select {
	case got := <-uCh:
		want.Dests = sets.NewString("10.0.0.2:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}
}

func TestRevisionDeleted(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	svc := privateSKSService(
		types.NamespacedName{testNamespace, testRevision},
		"129.0.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(svc)
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

	ei := fakeendpointsinformer.Get(ctx)
	ep := ep(testRevision, 1234, "http", "128.0.0.1")
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ep)
	waitInformers, err := controller.RunInformers(ctx.Done(), ei.Informer())
	if err != nil {
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	rev := revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	ri := fakerevisioninformer.Get(ctx)
	ri.Informer().GetIndexer().Add(rev)

	fakeRT := activatortest.FakeRoundTripper{}
	rt := network.RoundTripperFunc(fakeRT.RT)

	rbm := newRevisionBackendsManager(ctx, rt)
	// Make some movements.
	ei.Informer().GetIndexer().Add(ep)
	select {
	case <-rbm.updates():
	case <-time.After(time.Second * 2):
		t.Errorf("Timedout waiting for initial response")
	}
	// Now delete the endpoints.
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Delete(ep.Name, &metav1.DeleteOptions{})
	select {
	case r := <-rbm.updates():
		t.Errorf("Unexpected update: %#v", r)
	case <-time.After(time.Millisecond * 200):
		// Wait to make sure the callbacks are executed.
	}

	cancel()
	waitForRevisionBackedMananger(t, rbm)
}

func TestServiceDoesNotExist(t *testing.T) {
	// Tests when the service is not available.
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	ei := fakeendpointsinformer.Get(ctx)
	eps := ep(testRevision, 1234, "http", "128.0.0.1")
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(eps)
	waitInformers, err := controller.RunInformers(ctx.Done(), ei.Informer())
	if err != nil {
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	rev := revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	ri := fakerevisioninformer.Get(ctx)
	ri.Informer().GetIndexer().Add(rev)

	// This will make sure we go to the cluster IP probing.
	fakeRT := activatortest.FakeRoundTripper{
		ExpectHost: testRevision,
		ProbeHostResponses: map[string][]activatortest.FakeResponse{
			// To ensure that if we fail, when we get into the second iteration
			// of probing if the test is not yet complete, we store 2 items here.
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}},
		},
	}
	rt := network.RoundTripperFunc(fakeRT.RT)

	rbm := newRevisionBackendsManager(ctx, rt)
	// Make some movements to generate a checkDests call.
	ei.Informer().GetIndexer().Add(eps)
	select {
	case x := <-rbm.updates():
		// We can't probe endpoints (see RT above) and we can't get to probe
		// cluster IP. But if the service is accessible then we will and probing will
		// succeed since RT has no rules for that.
		t.Errorf("Unexpected update, should have had none: %v", x)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	waitForRevisionBackedMananger(t, rbm)
}

func TestServiceMoreThanOne(t *testing.T) {
	// Tests when the service is not available.
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	ei := fakeendpointsinformer.Get(ctx)
	eps := ep(testRevision, 1234, "http", "128.0.0.1")
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(eps)
	waitInformers, err := controller.RunInformers(ctx.Done(), ei.Informer())
	if err != nil {
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	rev := revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	ri := fakerevisioninformer.Get(ctx)
	ri.Informer().GetIndexer().Add(rev)

	// Now let's create two!
	for _, num := range []string{"11", "12"} {
		svc := privateSKSService(
			types.NamespacedName{testNamespace, testRevision},
			"129.0.0."+num,
			[]corev1.ServicePort{{Name: "http", Port: 1234}},
		)
		// Modify the name so both can be created.
		svc.Name = svc.Name + num
		fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(svc)
		si := fakeserviceinformer.Get(ctx)
		si.Informer().GetIndexer().Add(svc)
	}

	// Make sure fake probe failures ensue.
	fakeRT := activatortest.FakeRoundTripper{
		ExpectHost: testRevision,
		ProbeHostResponses: map[string][]activatortest.FakeResponse{
			// To ensure that if we fail, when we get into the second iteration
			// of probing if the test is not yet complete, we store 2 items here.
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}},
		},
	}

	rt := network.RoundTripperFunc(fakeRT.RT)
	rbm := newRevisionBackendsManager(ctx, rt)
	ei.Informer().GetIndexer().Add(eps)
	select {
	case x := <-rbm.updates():
		// We can't probe endpoints (see RT above) and we can't get to probe
		// cluster IP. But if the service is accessible then we will and probing will
		// succeed since RT has no rules for that.
		t.Errorf("Unexpected update, should have had none: %v", x)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	waitForRevisionBackedMananger(t, rbm)
}
