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
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"

	. "knative.dev/pkg/logging/testing"
)

const (
	testNamespace      = "test-namespace"
	testRevision       = "test-revision"
	informerRestPeriod = 500 * time.Millisecond
)

func revision(revID types.NamespacedName, protocol networking.ProtocolType) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revID.Namespace,
			Name:      revID.Name,
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1.RevisionSpec{
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
		probeResponses        []activatortest.FakeResponse
		initialClusterIPState bool
	}{{
		name:  "single healthy podIP",
		dests: []string{"128.0.0.1:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.1:1234")}},
		probeResponses: []activatortest.FakeResponse{{
			Err: errors.New("clusterIP transport error"),
		}, {
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
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{ClusterIPDest: "129.0.0.1:1234", Dests: sets.NewString("128.0.0.1:1234")}},
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
		probeResponses: []activatortest.FakeResponse{{
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
		clusterIP: "129.0.0.1",
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
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.1:1234")}},
		probeResponses: []activatortest.FakeResponse{{
			Err: errors.New("clusterIP transport error"),
		}, {
			Code: http.StatusServiceUnavailable,
			Body: queue.Name,
		}, {
			Err: errors.New("clusterIP transport error"),
		}, {
			Code: http.StatusOK,
			Body: queue.Name,
		}},
	}, {
		name:  "multiple healthy podIP",
		dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234")}},
		probeResponses: []activatortest.FakeResponse{{
			Err: errors.New("clusterIP transport error"),
		}, {
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
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.NewString("128.0.0.2:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.2:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "one healthy one unhealthy podIP then 2, then cluster",
		dests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 4321,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{
			{Dests: sets.NewString("128.0.0.2:1234")},
			{Dests: sets.NewString("128.0.0.2:1234", "128.0.0.1:1234")},
			{ClusterIPDest: "129.0.0.1:4321", Dests: sets.NewString("128.0.0.2:1234", "128.0.0.1:1234")},
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:4321": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Err: errors.New("podIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.2:1234": {{
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
		expectUpdates: []revisionDestsUpdate{
			{Dests: sets.NewString("128.0.0.1:1234")},
			{Dests: sets.NewString("128.0.0.1:1234"), ClusterIPDest: "129.0.0.1:1234"},
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusServiceUnavailable,
				Body: queue.Name,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			fakeRT := activatortest.FakeRoundTripper{
				ExpectHost:         testRevision,
				ProbeHostResponses: tc.probeHostResponses,
				ProbeResponses:     tc.probeResponses,
			}
			rt := network.RoundTripperFunc(fakeRT.RT)

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()

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

			// Shutdown run loop.
			cancel()

			wg.Wait()

			// Autofill out Rev in expectUpdates
			for i := range tc.expectUpdates {
				tc.expectUpdates[i].Rev = revID
			}

			if got, want := updates, tc.expectUpdates; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Errorf("revisionDests updates = %v, want: %v, diff (-want, +got):\n %s", got, want, cmp.Diff(want, got))
			}

			assertChClosed(t, destsCh)
		})
	}
}

func assertChClosed(t *testing.T, ch chan sets.String) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("the channel was not closed")
		}
	}()
	select {
	case ch <- nil:
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
	defer time.Sleep(informerRestPeriod)
	for _, tc := range []struct {
		name               string
		endpointsArr       []*corev1.Endpoints
		revisions          []*v1alpha1.Revision
		services           []*corev1.Service
		probeResponses     []activatortest.FakeResponse
		probeHostResponses map[string][]activatortest.FakeResponse
		expectDests        map[types.NamespacedName]revisionDestsUpdate
		updateCnt          int
	}{{
		name:         "add slow healthy",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
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
			revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolH2C),
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
			revision(types.NamespacedName{testNamespace, "test-revision1"}, networking.ProtocolHTTP1),
			revision(types.NamespacedName{testNamespace, "test-revision2"}, networking.ProtocolHTTP1),
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
		name:         "slow podIP then clusterIP",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{testNamespace, testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
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
				ClusterIPDest: "129.0.0.1:1234",
				Dests:         sets.NewString("128.0.0.1:1234"),
			},
		},
		updateCnt: 2,
	}, {
		name:         "unhealthy",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1alpha1.Revision{
			revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
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
				ProbeResponses:     tc.probeResponses,
			}
			rt := network.RoundTripperFunc(fakeRT.RT)

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()

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

			controller.StartInformers(ctx.Done(), endpointsInformer.Informer())

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
		})
	}
}

func TestCheckDests(t *testing.T) {
	// This test covers some edge cases in `checkDests` which are next to impossible to
	// test via tests above.

	// To make sure context switch happens and informers terminate.
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer func() {
		cancel()
		time.Sleep(informerRestPeriod)
	}()

	svc := privateSKSService(
		types.NamespacedName{testNamespace, testRevision},
		"129.0.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(svc)
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)
	// Make it buffered,so that we can make the test linear.
	uCh := make(chan revisionDestsUpdate, 1)
	dCh := make(chan struct{})
	rw := &revisionWatcher{
		clusterIPHealthy: true,
		rev:              types.NamespacedName{testNamespace, testRevision},
		updateCh:         uCh,
		serviceLister:    si.Lister(),
		logger:           TestLogger(t),
		doneCh:           dCh,
	}
	rw.checkDests(sets.NewString("10.1.1.5"))
	select {
	case <-uCh:
		// success.
	default:
		t.Error("Expected update but it never went out.")
	}

	close(dCh)
	rw.checkDests(sets.NewString("10.1.1.5"))
	select {
	case <-uCh:
		t.Error("Expected no update but got one")
	default:
		// success.
	}
}

func TestCheckDestsSwinging(t *testing.T) {
	// This test permits us to test the case when endpoints actually change
	// underneath (e.g. pod crash/restart).
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer func() {
		cancel()
		time.Sleep(informerRestPeriod)
	}()

	svc := privateSKSService(
		types.NamespacedName{testNamespace, testRevision},
		"10.5.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)

	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(svc)
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

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
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
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
		},
	}

	// Make it buffered,so that we can make the test linear.
	uCh := make(chan revisionDestsUpdate, 1)
	dCh := make(chan struct{})
	rw := &revisionWatcher{
		rev:           types.NamespacedName{testNamespace, testRevision},
		updateCh:      uCh,
		serviceLister: si.Lister(),
		logger:        TestLogger(t),
		doneCh:        dCh,
		transport:     network.RoundTripperFunc(fakeRT.RT),
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

	// And finally the cluster IP is ready.
	rw.checkDests(sets.NewString("10.0.0.1:1234", "10.0.0.2:1234"))
	want.ClusterIPDest = "10.5.0.1:1234"
	select {
	case got := <-uCh:
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}
}

func TestRevisionDeleted(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer func() {
		cancel()
		time.Sleep(informerRestPeriod)
	}()

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
	controller.StartInformers(ctx.Done(), ei.Informer())

	rev := revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1)
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
}
