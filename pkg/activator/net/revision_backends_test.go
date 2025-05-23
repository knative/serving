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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	pkgnet "knative.dev/networking/pkg/apis/networking"
	netcfg "knative.dev/networking/pkg/config"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	pkgnetwork "knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"

	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"

	. "knative.dev/pkg/logging/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"

	probeFreq     = 50 * time.Millisecond
	updateTimeout = 16 * probeFreq

	meshErrorStatusCode = http.StatusServiceUnavailable
)

// revisionCC1 - creates a revision with concurrency == 1.
func revisionCC1(revID types.NamespacedName, protocol pkgnet.ProtocolType) *v1.Revision {
	return revision(revID, protocol, 1)
}

func revision(revID types.NamespacedName, protocol pkgnet.ProtocolType, cc int64, options ...func(r *v1.Revision)) *v1.Revision {
	r := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revID.Namespace,
			Name:      revID.Name,
		},
		Spec: v1.RevisionSpec{
			ContainerConcurrency: ptr.Int64(cc),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Ports: []corev1.ContainerPort{{
						Name: string(protocol),
					}},
				}},
			},
		},
	}
	for _, o := range options {
		o(r)
	}
	return r
}

func privateSKSService(revID types.NamespacedName, clusterIP string, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: revID.Namespace,
			Name:      names.PrivateService(revID.Name),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: clusterIP,
			Ports:     ports,
		},
	}
}

func waitForRevisionBackendManager(t *testing.T, rbm *revisionBackendsManager) {
	timeout := time.After(updateTimeout)
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
		dests                 dests
		protocol              pkgnet.ProtocolType
		clusterPort           corev1.ServicePort
		clusterIP             string
		expectUpdates         []revisionDestsUpdate
		probeHostResponses    map[string][]activatortest.FakeResponse
		initialClusterIPState bool
		noPodAddressability   bool // This keeps the test defs shorter.
		usePassthroughLb      bool
		meshMode              netcfg.MeshCompatibilityMode
	}{{
		name:  "single healthy podIP",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.New("128.0.0.1:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "single not ready but healthy podIP",
		dests: dests{notReady: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.New("128.0.0.1:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:     "single http2 podIP",
		dests:    dests{ready: sets.New("128.0.0.1:1234")},
		protocol: pkgnet.ProtocolH2C,
		clusterPort: corev1.ServicePort{
			Name: "http2",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.New("128.0.0.1:1234")}},
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
		dests:    dests{ready: sets.New("128.0.0.1:1234"), notReady: sets.New("128.0.0.2:1234")},
		protocol: pkgnet.ProtocolH2C,
		clusterPort: corev1.ServicePort{
			Name: "http2",
			Port: 1234,
		},
		clusterIP:           "129.0.0.1",
		noPodAddressability: true,
		expectUpdates:       []revisionDestsUpdate{{ClusterIPDest: "129.0.0.1:1234", Dests: sets.New("128.0.0.1:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Code: meshErrorStatusCode,
			}},
			"128.0.0.2:1234": {{
				Code: meshErrorStatusCode,
			}},
		},
	}, {
		name:      "no pods",
		dests:     dests{},
		clusterIP: "129.0.0.1",
	}, {
		name:                  "no pods, was happy",
		dests:                 dests{},
		clusterIP:             "129.0.0.1",
		initialClusterIPState: true,
	}, {
		name:  "single unavailable podIP",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
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
		dests: dests{ready: sets.New("128.0.0.1:1234")},
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
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.New("128.0.0.1:1234")}},
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
		dests: dests{ready: sets.New("128.0.0.1:1234", "128.0.0.2:1234"), notReady: sets.New("128.0.0.3:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.New("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.3:1234")}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.2:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.3:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "one healthy one unhealthy podIP",
		dests: dests{ready: sets.New("128.0.0.1:1234", "128.0.0.2:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP:     "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{Dests: sets.New("128.0.0.2:1234")}},
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
		dests: dests{ready: sets.New("128.0.0.1:1234", "128.0.0.2:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 4321,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{
			{Dests: sets.New("128.0.0.2:1234")},
			{Dests: sets.New("128.0.0.2:1234", "128.0.0.1:1234")},
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
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1234,
		},
		clusterIP: "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.New("128.0.0.1:1234"),
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
				Code: meshErrorStatusCode,
			}, {
				Code: meshErrorStatusCode,
			}},
		},
	}, {
		name:  "clusterIP ready, no pod addressability",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1235,
		},
		noPodAddressability: true,
		clusterIP:           "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{{
			ClusterIPDest: "129.0.0.1:1235",
			Dests:         sets.New("128.0.0.1:1234"),
		}},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Code: meshErrorStatusCode,
			}},
		},
	}, {
		name:  "clusterIP ready, pod fails with non-mesh error then succeeds",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1235,
		},
		noPodAddressability: false,
		clusterIP:           "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{
			{Dests: sets.New("128.0.0.1:1234")},
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				// ClusterIP is healthy, but should not be used.
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusInternalServerError,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "passthrough lb, clusterIP ready but no fallback",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1235,
		},
		noPodAddressability: true,
		clusterIP:           "129.0.0.1",
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Code: meshErrorStatusCode,
			}},
		},
		usePassthroughLb: true,
	}, {
		name:  "mesh mode enabled: pod ready but should still use cluster IP",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1235,
		},
		noPodAddressability: true,
		clusterIP:           "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{
			{ClusterIPDest: "129.0.0.1:1235", Dests: sets.New("128.0.0.1:1234")},
		},
		meshMode: netcfg.MeshCompatibilityModeEnabled,
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				// Pod is healthy, but should not be used since we're in mesh mode.
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"129.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "mesh mode disabled: pod initially returns mesh-compatible error, but don't fallback",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1235,
		},
		noPodAddressability: false,
		clusterIP:           "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{
			{Dests: sets.New("128.0.0.1:1234")},
		},
		meshMode: netcfg.MeshCompatibilityModeDisabled,
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				// Cluster IP healthy, but should not be used.
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				// Mesh compatible error, but don't fall back (because mesh mode is disabled).
				Code: meshErrorStatusCode,
				Body: queue.Name,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
	}, {
		name:  "ready pod in k8s api when mesh-compat disabled",
		dests: dests{ready: sets.New("128.0.0.1:1234")},
		clusterPort: corev1.ServicePort{
			Name: "http",
			Port: 1235,
		},
		noPodAddressability: false,
		clusterIP:           "129.0.0.1",
		expectUpdates: []revisionDestsUpdate{
			{Dests: sets.New("128.0.0.1:1234")},
		},
		meshMode: netcfg.MeshCompatibilityModeDisabled,
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"128.0.0.1:1234": {{
				// Probe errors, but it shouldn't matter because we should trust
				// k8s here and not bother probing.
				Code: meshErrorStatusCode,
				Body: queue.Name,
			}},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			fakeRT := activatortest.FakeRoundTripper{
				ExpectHost:         testRevision,
				ProbeHostResponses: tc.probeHostResponses,
			}
			rt := pkgnetwork.RoundTripperFunc(fakeRT.RT)

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

			updateCh := make(chan revisionDestsUpdate, len(tc.expectUpdates)+1)

			// This gets closed up by revisionWatcher.
			destsCh := make(chan dests)

			// Default for protocol is HTTP1.
			if tc.protocol == "" {
				tc.protocol = pkgnet.ProtocolHTTP1
			}

			// Default for meshMode is auto.
			if tc.meshMode == "" {
				tc.meshMode = netcfg.MeshCompatibilityModeAuto
			}

			fake := fakekubeclient.Get(ctx)
			informer := fakeserviceinformer.Get(ctx)

			revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
			if tc.clusterIP != "" {
				svc := privateSKSService(revID, tc.clusterIP, []corev1.ServicePort{tc.clusterPort})
				fake.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
				informer.Informer().GetIndexer().Add(svc)
			}

			waitInformers, err := rtesting.RunAndSyncInformers(ctx, informer.Informer())
			if err != nil {
				t.Fatal("Failed to start informers:", err)
			}
			defer func() {
				cancel()
				waitInformers()
				close(updateCh)
			}()

			rw := newRevisionWatcher(
				ctx,
				revID,
				tc.protocol,
				updateCh,
				destsCh,
				rt,
				informer.Lister(),
				tc.usePassthroughLb, // usePassthroughLb
				tc.meshMode,
				true,
				logger)
			rw.clusterIPHealthy = tc.initialClusterIPState

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				rw.run(probeFreq)
			}()

			destsCh <- tc.dests

			updates := []revisionDestsUpdate{}
			for range len(tc.expectUpdates) {
				select {
				case update := <-updateCh:
					updates = append(updates, update)
				case <-time.After(updateTimeout):
					t.Fatal("Timed out waiting for update event")
				}
			}
			if got, want := rw.podsAddressable, !tc.noPodAddressability || tc.usePassthroughLb; got != want {
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
				t.Errorf("revisionDests updates = %v, want: %v, diff(-want, +got):\n%s", got, want, cmp.Diff(want, got))
			}
		})
	}
}

func assertChClosed(t *testing.T, ch chan struct{}) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The channel was not closed")
		}
	}()
	select {
	case ch <- struct{}{}:
		// Panics if the channel is closed
	default:
		// Prevents from blocking forever if the channel is not closed
	}
}

func epSubset(port int32, portName string, ips, notReadyIps []string) *corev1.EndpointSubset {
	ss := &corev1.EndpointSubset{
		Ports: []corev1.EndpointPort{{
			Name: portName,
			Port: port,
		}},
	}
	for _, ip := range ips {
		ss.Addresses = append(ss.Addresses, corev1.EndpointAddress{IP: ip})
	}
	for _, notReady := range notReadyIps {
		ss.NotReadyAddresses = append(ss.NotReadyAddresses, corev1.EndpointAddress{IP: notReady})
	}
	return ss
}

func ep(revL string, port int32, portName string, ips ...string) *corev1.Endpoints {
	return epNotReady(revL, port, portName, ips, nil)
}

func epNotReady(revL string, port int32, portName string, readyIps, notReadyIps []string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: revL + "-ep",
			Labels: map[string]string{
				serving.RevisionUID:       time.Now().Format("150415.000"),
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
				serving.RevisionLabelKey:  revL,
			},
		},
		Subsets: []corev1.EndpointSubset{*epSubset(port, portName, readyIps, notReadyIps)},
	}
}

func TestRevisionBackendManagerAddEndpoint(t *testing.T) {
	// Make sure we wait out all the jitter in the system.
	for _, tc := range []struct {
		name               string
		endpointsArr       []*corev1.Endpoints
		revisions          []*v1.Revision
		services           []*corev1.Service
		probeHostResponses map[string][]activatortest.FakeResponse
		expectDests        map[types.NamespacedName]revisionDestsUpdate
		updateCnt          int
	}{{
		name:         "add slow healthy",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1.Revision{
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
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
				Dests: sets.New("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name:         "add slow ready http2",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http2", "128.0.0.1")},
		revisions: []*v1.Revision{
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolH2C),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
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
				Dests: sets.New("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name: "multiple revisions",
		endpointsArr: []*corev1.Endpoints{
			ep("test-revision1", 1234, "http", "128.0.0.1"),
			ep("test-revision2", 1235, "http", "128.1.0.2"),
		},
		revisions: []*v1.Revision{
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: "test-revision1"}, pkgnet.ProtocolHTTP1),
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: "test-revision2"}, pkgnet.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: "test-revision1"}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 2345}}),
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: "test-revision2"}, "129.0.0.2",
				[]corev1.ServicePort{{Name: "http", Port: 2345}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:2345": {{Err: errors.New("clusterIP transport error")}},
			"129.0.0.2:2345": {{Err: errors.New("clusterIP transport error")}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: "test-revision1"}: {
				Dests: sets.New("128.0.0.1:1234"),
			},
			{Namespace: testNamespace, Name: "test-revision2"}: {
				Dests: sets.New("128.1.0.2:1235"),
			},
		},
		updateCnt: 2,
	}, {
		name:         "no pods available, but non-mesh-related error",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1.Revision{
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{ // Should not succeed by hitting this cluster IP.
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusNotFound, // Not 503 error, so should not cause fall-back to mesh.
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{},
		updateCnt:   0,
	}, {
		name:         "no pod addressability",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1.Revision{
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
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
				Dests:         sets.New("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name:         "unhealthy",
		endpointsArr: []*corev1.Endpoints{ep(testRevision, 1234, "http", "128.0.0.1")},
		revisions: []*v1.Revision{
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
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
	}, {
		name:         "unready pod successfully probed",
		endpointsArr: []*corev1.Endpoints{epNotReady(testRevision, 1234, "http", nil, []string{"128.0.0.1"})},
		revisions: []*v1.Revision{
			revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: testRevision}: {
				Dests: sets.New("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name:         "pod with exec probe only goes ready when kubernetes agrees",
		endpointsArr: []*corev1.Endpoints{epNotReady(testRevision, 1234, "http", nil, []string{"128.0.0.1"})},
		revisions: []*v1.Revision{
			revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1, 1, func(r *v1.Revision) {
				r.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{},
					},
				}
			}),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{},
		updateCnt:   0,
	}, {
		name:         "pod with exec probe goes ready when kubernetes agrees",
		endpointsArr: []*corev1.Endpoints{epNotReady(testRevision, 1234, "http", []string{"128.0.0.1"}, nil)},
		revisions: []*v1.Revision{
			revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1, 1, func(r *v1.Revision) {
				r.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{},
					},
				}
			}),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: testRevision}: {
				Dests: sets.New("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}, {
		name:         "pod with sidecar container with exec probe only goes ready when kubernetes agrees",
		endpointsArr: []*corev1.Endpoints{epNotReady(testRevision, 1234, "http", nil, []string{"128.0.0.1"})},
		revisions: []*v1.Revision{
			revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1, 1, func(r *v1.Revision) {
				r.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt32(8080),
						},
					},
				}
				r.Spec.Containers = append(r.Spec.Containers, corev1.Container{
					Name: "sidecar",
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{},
						},
					},
				})
			}),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{},
		updateCnt:   0,
	}, {
		name:         "pod with sidecar container with exec probe goes ready when kubernetes agrees",
		endpointsArr: []*corev1.Endpoints{epNotReady(testRevision, 1234, "http", []string{"128.0.0.1"}, nil)},
		revisions: []*v1.Revision{
			revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1, 1, func(r *v1.Revision) {
				//
				r.Spec.Containers = append(r.Spec.Containers, corev1.Container{
					Name: "sidecar",
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{},
						},
					},
				})
			}),
		},
		services: []*corev1.Service{
			privateSKSService(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, "129.0.0.1",
				[]corev1.ServicePort{{Name: "http", Port: 1234}}),
		},
		probeHostResponses: map[string][]activatortest.FakeResponse{
			"129.0.0.1:1234": {{
				Err: errors.New("clusterIP transport error"),
			}},
			"128.0.0.1:1234": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
		},
		expectDests: map[types.NamespacedName]revisionDestsUpdate{
			{Namespace: testNamespace, Name: testRevision}: {
				Dests: sets.New("128.0.0.1:1234"),
			},
		},
		updateCnt: 1,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			fakeRT := activatortest.FakeRoundTripper{
				ExpectHost:         testRevision,
				ProbeHostResponses: tc.probeHostResponses,
			}
			rt := pkgnetwork.RoundTripperFunc(fakeRT.RT)

			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

			endpointsInformer := fakeendpointsinformer.Get(ctx)
			serviceInformer := fakeserviceinformer.Get(ctx)
			revisions := fakerevisioninformer.Get(ctx)

			// Add the revision we're testing.
			for _, rev := range tc.revisions {
				fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(ctx, rev, metav1.CreateOptions{})
				revisions.Informer().GetIndexer().Add(rev)
			}

			for _, svc := range tc.services {
				fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(ctx, svc, metav1.CreateOptions{})
				serviceInformer.Informer().GetIndexer().Add(svc)
			}

			waitInformers, err := rtesting.RunAndSyncInformers(ctx, endpointsInformer.Informer())
			if err != nil {
				t.Fatal("Failed to start informers:", err)
			}

			rbm := newRevisionBackendsManagerWithProbeFrequency(ctx, rt, false /*usePassthroughLb*/, netcfg.MeshCompatibilityModeAuto, probeFreq)
			defer func() {
				cancel()
				waitInformers()
				waitForRevisionBackendManager(t, rbm)
			}()

			for _, ep := range tc.endpointsArr {
				fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ctx, ep, metav1.CreateOptions{})
				endpointsInformer.Informer().GetIndexer().Add(ep)
			}

			revDests := make(map[types.NamespacedName]revisionDestsUpdate)
			// Wait for updateCb to be called
			for range tc.updateCnt {
				select {
				case update := <-rbm.updates():
					revDests[update.Rev] = update
				case <-time.After(updateTimeout):
					t.Error("Timed out waiting for update event")
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

func emptyDests() dests {
	return dests{
		ready:    sets.New[string](),
		notReady: sets.New[string](),
	}
}

func TestCheckDestsReadyToNotReady(t *testing.T) {
	// This test verifies the edge behaviour when a pod
	// previously in the ready sed moved to non ready set
	// and now must be re-probed.
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	svc := privateSKSService(
		types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		"129.0.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(ctx, svc, metav1.CreateOptions{})
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

	waitInformers, err := rtesting.RunAndSyncInformers(ctx, si.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	fakeRT := activatortest.FakeRoundTripper{
		ExpectHost: testRevision,
		ProbeHostResponses: map[string][]activatortest.FakeResponse{
			"10.10.1.1": {{
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				Err: errors.New("clusterIP transport error"),
			}, {
				// Finally!
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"10.10.1.2": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}, {
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			"10.10.1.3": {{
				Code: http.StatusOK,
				Body: queue.Name,
			}, {
				Err: errors.New("clusterIP transport error"),
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
		clusterIPHealthy:        true,
		podsAddressable:         true,
		rev:                     types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		updateCh:                uCh,
		serviceLister:           si.Lister(),
		logger:                  TestLogger(t),
		stopCh:                  dCh,
		transport:               pkgnetwork.RoundTripperFunc(fakeRT.RT),
		meshMode:                netcfg.MeshCompatibilityModeAuto,
		enableProbeOptimisation: true,
	}
	// Initial state. Both are ready.
	cur := dests{
		ready:    sets.New("10.10.1.3", "10.10.1.2"),
		notReady: sets.New("10.10.1.1"),
	}
	rw.checkDests(cur, emptyDests())
	select {
	case u := <-uCh:
		if got, want := len(u.Dests), 2; got != want {
			t.Fatalf("NumReady = %d, want: %d, update.Dests = %v", got, want, u.Dests)
		}
	default:
		t.Error("Expected update but it never went out.")
	}
	// Repeating should be a no-op.
	rw.checkDests(cur, emptyDests())
	select {
	case u := <-uCh:
		t.Fatal("Unexpected update", u)
	default:
		// Success.
	}

	// Now swing the ready to the non ready, and it should fail the probe.
	prev := cur

	cur = dests{
		ready:    sets.New("10.10.1.2"),
		notReady: sets.New("10.10.1.1", "10.10.1.3"),
	}
	rw.checkDests(cur, prev)
	select {
	case u := <-uCh:
		// The 10.10.1.3 should be not ready now.
		if got, want := len(u.Dests), 1; got != want {
			t.Fatalf("NumReady = %d, want: %d, update.Dests = %v", got, want, u.Dests)
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Now just re-probe the same and it should succeed (see the RT setup above).
	rw.checkDests(cur, prev)
	select {
	case u := <-uCh:
		if got, want := len(u.Dests), 2; got != want {
			t.Fatalf("NumReady = %d, want: %d, update.Dests = %v", got, want, u.Dests)
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Timer update.
	prev.ready.Delete("10.10.1.3")
	// The last one should finally succeed.
	rw.checkDests(cur, prev)
	select {
	case u := <-uCh:
		if got, want := len(u.Dests), 3; got != want {
			t.Fatalf("NumReady = %d, want: %d, update.Dests = %v", got, want, u.Dests)
		}
	default:
		t.Error("Expected update but it never went out.")
	}
}

func TestCheckDests(t *testing.T) {
	// This test covers some edge cases in `checkDests` which are next to impossible to
	// test via tests above.

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	svc := privateSKSService(
		types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		"129.0.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(ctx, svc, metav1.CreateOptions{})
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

	waitInformers, err := rtesting.RunAndSyncInformers(ctx, si.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Make it buffered,so that we can make the test linear.
	uCh := make(chan revisionDestsUpdate, 1)
	dCh := make(chan struct{})
	rw := &revisionWatcher{
		clusterIPHealthy:        true,
		podsAddressable:         false,
		rev:                     types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		updateCh:                uCh,
		serviceLister:           si.Lister(),
		logger:                  TestLogger(t),
		stopCh:                  dCh,
		enableProbeOptimisation: true,
	}
	rw.checkDests(dests{
		ready:    sets.New("10.1.1.5"),
		notReady: sets.New("10.1.1.6"),
	}, emptyDests())
	select {
	case <-uCh:
		// Success.
	default:
		t.Error("Expected update but it never went out.")
	}

	close(dCh)
	rw.checkDests(dests{
		ready:    sets.New("10.1.1.5"),
		notReady: sets.New("10.1.1.6"),
	}, emptyDests())
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
		types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		"10.5.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)

	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(ctx, svc, metav1.CreateOptions{})
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

	waitInformers, err := rtesting.RunAndSyncInformers(ctx, si.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
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
			"10.0.0.4:1234": {{
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
		rev:                     types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		updateCh:                uCh,
		serviceLister:           si.Lister(),
		logger:                  TestLogger(t),
		stopCh:                  dCh,
		podsAddressable:         true,
		transport:               pkgnetwork.RoundTripperFunc(fakeRT.RT),
		meshMode:                netcfg.MeshCompatibilityModeAuto,
		enableProbeOptimisation: true,
	}

	// First not ready, second good, clusterIP: not ready.
	rw.checkDests(dests{ready: sets.New("10.0.0.1:1234", "10.0.0.2:1234")}, emptyDests())
	want := revisionDestsUpdate{
		Rev:           types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		ClusterIPDest: "",
		Dests:         sets.New("10.0.0.2:1234"),
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
	rw.checkDests(dests{ready: sets.New("10.0.0.1:1234")}, emptyDests())
	select {
	case got := <-uCh:
		want.Dests = sets.New("10.0.0.1:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Second is back, but not healthy yet.
	rw.checkDests(dests{ready: sets.New("10.0.0.1:1234", "10.0.0.2:1234")}, emptyDests())
	select {
	case got := <-uCh:
		// No update should be sent out, since there's only healthy pod, same as above.
		t.Errorf("Got = %#v, expected no update", got)
	default:
	}

	// All pods are happy now.
	rw.checkDests(dests{ready: sets.New("10.0.0.1:1234", "10.0.0.2:1234")}, emptyDests())
	select {
	case got := <-uCh:
		want.Dests = sets.New("10.0.0.2:1234", "10.0.0.1:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Make sure we do not send out redundant updates.
	rw.checkDests(dests{ready: sets.New("10.0.0.1:1234", "10.0.0.2:1234")}, emptyDests())
	select {
	case got := <-uCh:
		t.Errorf("Expected no update, but got %#v", got)
	default:
		// Success.
	}

	// Add a notReady pod, but it's not ready. No update should be sent.
	rw.checkDests(dests{
		ready:    sets.New("10.0.0.1:1234", "10.0.0.2:1234"),
		notReady: sets.New("10.0.0.4:1234"),
	}, emptyDests())
	select {
	case got := <-uCh:
		t.Errorf("Expected no update, but got %#v", got)
	default:
		// Success.
	}

	// The notReady pod is now ready!
	rw.checkDests(dests{
		ready:    sets.New("10.0.0.1:1234", "10.0.0.2:1234"),
		notReady: sets.New("10.0.0.4:1234"),
	}, emptyDests())
	select {
	case got := <-uCh:
		want.Dests = sets.New("10.0.0.1:1234", "10.0.0.2:1234", "10.0.0.4:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Swing to a different pods.
	rw.checkDests(dests{ready: sets.New("10.0.0.3:1234", "10.0.0.2:1234")}, emptyDests())
	select {
	case got := <-uCh:
		want.Dests = sets.New("10.0.0.2:1234", "10.0.0.3:1234")
		if !cmp.Equal(got, want) {
			t.Errorf("Update = %#v, want: %#v, diff: %s", got, want, cmp.Diff(want, got))
		}
	default:
		t.Error("Expected update but it never went out.")
	}

	// Scale down by 1.
	rw.checkDests(dests{ready: sets.New("10.0.0.2:1234")}, emptyDests())
	select {
	case got := <-uCh:
		want.Dests = sets.New("10.0.0.2:1234")
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
		types.NamespacedName{Namespace: testNamespace, Name: testRevision},
		"129.0.0.1",
		[]corev1.ServicePort{{Name: "http", Port: 1234}},
	)
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(ctx, svc, metav1.CreateOptions{})
	si := fakeserviceinformer.Get(ctx)
	si.Informer().GetIndexer().Add(svc)

	ei := fakeendpointsinformer.Get(ctx)
	ep := ep(testRevision, 1234, "http", "128.0.0.1")
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ctx, ep, metav1.CreateOptions{})
	waitInformers, err := rtesting.RunAndSyncInformers(ctx, ei.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	rev := revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(ctx, rev, metav1.CreateOptions{})
	ri := fakerevisioninformer.Get(ctx)
	ri.Informer().GetIndexer().Add(rev)

	fakeRT := activatortest.FakeRoundTripper{}
	rbm := newRevisionBackendsManagerWithProbeFrequency(ctx, pkgnetwork.RoundTripperFunc(fakeRT.RT), false /*usePassthroughLb*/, netcfg.MeshCompatibilityModeAuto, probeFreq)
	defer func() {
		cancel()
		waitInformers()
		waitForRevisionBackendManager(t, rbm)
	}()

	// Make some movements.
	ei.Informer().GetIndexer().Add(ep)
	select {
	case <-rbm.updates():
	case <-time.After(updateTimeout):
		t.Error("Timedout waiting for initial response")
	}
	// Now delete the endpoints.
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Delete(ctx, ep.Name, metav1.DeleteOptions{})
	select {
	case r := <-rbm.updates():
		t.Errorf("Unexpected update: %#v", r)
	case <-time.After(updateTimeout):
		// Wait to make sure the callbacks are executed.
	}
}

func TestServiceDoesNotExist(t *testing.T) {
	// Tests when the service is not available.
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	ei := fakeendpointsinformer.Get(ctx)
	eps := ep(testRevision, 1234, "http", "128.0.0.1")
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ctx, eps, metav1.CreateOptions{})
	waitInformers, err := rtesting.RunAndSyncInformers(ctx, ei.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	rev := revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(ctx, rev, metav1.CreateOptions{})
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
	rbm := newRevisionBackendsManagerWithProbeFrequency(ctx, pkgnetwork.RoundTripperFunc(fakeRT.RT), false /*usePassthroughLb*/, netcfg.MeshCompatibilityModeAuto, probeFreq)
	defer func() {
		cancel()
		waitInformers()
		waitForRevisionBackendManager(t, rbm)
	}()

	// Make some movements to generate a checkDests call.
	ei.Informer().GetIndexer().Add(eps)
	select {
	case x := <-rbm.updates():
		// We can't probe endpoints (see RT above) and we can't get to probe
		// cluster IP. But if the service is accessible then we will and probing will
		// succeed since RT has no rules for that.
		t.Error("Unexpected update, should have had none:", x)
	case <-time.After(updateTimeout):
	}
}

func TestServiceMoreThanOne(t *testing.T) {
	// Tests when the service is not available.
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	ei := fakeendpointsinformer.Get(ctx)
	eps := ep(testRevision, 1234, "http", "128.0.0.1")
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ctx, eps, metav1.CreateOptions{})
	waitInformers, err := rtesting.RunAndSyncInformers(ctx, ei.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	rev := revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(ctx, rev, metav1.CreateOptions{})
	ri := fakerevisioninformer.Get(ctx)
	ri.Informer().GetIndexer().Add(rev)

	// Now let's create two!
	for _, num := range []string{"11", "12"} {
		svc := privateSKSService(
			types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			"129.0.0."+num,
			[]corev1.ServicePort{{Name: "http", Port: 1234}},
		)
		// Modify the name so both can be created.
		svc.Name += num
		fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(ctx, svc, metav1.CreateOptions{})
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
	rbm := newRevisionBackendsManagerWithProbeFrequency(ctx, pkgnetwork.RoundTripperFunc(fakeRT.RT), false /*usePassthroughLb*/, netcfg.MeshCompatibilityModeAuto, probeFreq)
	defer func() {
		cancel()
		waitInformers()
		waitForRevisionBackendManager(t, rbm)
	}()

	ei.Informer().GetIndexer().Add(eps)
	select {
	case x := <-rbm.updates():
		// We can't probe endpoints (see RT above) and we can't get to probe
		// cluster IP. But if the service is accessible then we will and probing will
		// succeed since RT has no rules for that.
		t.Errorf("Unexpected update, should have had none: %#v", x)
	case <-time.After(updateTimeout):
	}
}

// More focused test around the probing of pods and the handling of different behaviors
func TestProbePodIPs(t *testing.T) {
	type input struct {
		current                 dests
		healthy                 sets.Set[string]
		meshMode                netcfg.MeshCompatibilityMode
		enableProbeOptimization bool
		hostResponses           map[string][]activatortest.FakeResponse
	}

	type expected struct {
		healthy   sets.Set[string]
		noop      bool
		notMesh   bool
		success   bool
		numProbes int32
	}

	type test struct {
		name     string
		input    input
		expected expected
	}

	tests := []test{
		{
			name: "all healthy", // Test skipping probes when all endpoints are healthy
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.2"),
				},
				healthy: sets.New("10.10.1.1", "10.10.1.2"),
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1", "10.10.1.2"),
				noop:      true,
				notMesh:   false,
				success:   true,
				numProbes: 0,
			},
		},
		{
			name: "one pod fails probe", // Test that we probe all pods when one fails
			input: input{
				current: dests{
					notReady: sets.New("10.10.1.1", "10.10.1.2", "10.10.1.3"),
				},
				hostResponses: map[string][]activatortest.FakeResponse{
					"10.10.1.1": {{
						Err: errors.New("podIP transport error"),
					}},
					"10.10.1.2": {{
						Code:  http.StatusOK,
						Body:  queue.Name,
						Delay: 250 * time.Millisecond,
					}},
				},
				enableProbeOptimization: true,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.2", "10.10.1.3"),
				noop:      false,
				notMesh:   true,
				success:   false,
				numProbes: 3,
			},
		},
		{
			name: "ready pods skipped with mesh disabled",
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.2"),
				},
				enableProbeOptimization: true,
				meshMode:                netcfg.MeshCompatibilityModeDisabled,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1", "10.10.1.2"),
				noop:      false,
				notMesh:   true,
				success:   true,
				numProbes: 1,
			},
		},
		{
			name: "ready pods not skipped with mesh auto",
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.2"),
				},
				enableProbeOptimization: true,
				meshMode:                netcfg.MeshCompatibilityModeAuto,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1", "10.10.1.2"),
				noop:      false,
				notMesh:   true,
				success:   true,
				numProbes: 2,
			},
		},
		{
			name: "only ready pods healthy without probe optimization", // NOTE: prior test is effectively this one with probe optimization
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.2"),
				},
				enableProbeOptimization: false,
				meshMode:                netcfg.MeshCompatibilityModeAuto,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1"),
				noop:      false,
				notMesh:   true,
				success:   true,
				numProbes: 2,
			},
		},
		{
			name: "removes non-existent pods from healthy",
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.2"),
				},
				healthy:                 sets.New("10.10.1.1", "10.10.1.2", "10.10.1.3"),
				enableProbeOptimization: true,
				meshMode:                netcfg.MeshCompatibilityModeDisabled,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1", "10.10.1.2"),
				noop:      false,
				notMesh:   false,
				success:   true,
				numProbes: 0,
			},
		},
		{
			name: "non-probe additions count as changes", // Testing case where ready pods are added but probes do not add more
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1", "10.10.1.2"),
					notReady: sets.New("10.10.1.3"),
				},
				healthy:                 sets.New("10.10.1.1"),
				enableProbeOptimization: false,
				meshMode:                netcfg.MeshCompatibilityModeDisabled,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1", "10.10.1.2"),
				noop:      false,
				notMesh:   true,
				success:   true,
				numProbes: 1,
			},
		},
		{
			name: "non-probe removals count as changes", // Testing case where non-existent pods are removed with no probe changes
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.3"),
				},
				healthy:                 sets.New("10.10.1.1", "10.10.1.2"),
				enableProbeOptimization: false,
				meshMode:                netcfg.MeshCompatibilityModeDisabled,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1"),
				noop:      false,
				notMesh:   true,
				success:   true,
				numProbes: 1,
			},
		},
		{
			name: "no changes with probes",
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.3"),
				},
				healthy:                 sets.New("10.10.1.1"),
				enableProbeOptimization: false,
				meshMode:                netcfg.MeshCompatibilityModeDisabled,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1"),
				noop:      true,
				notMesh:   true,
				success:   true,
				numProbes: 1,
			},
		},
		{
			name: "no changes without probes",
			input: input{
				current: dests{
					ready: sets.New("10.10.1.1", "10.10.1.3"),
				},
				healthy:                 sets.New("10.10.1.1", "10.10.1.3"),
				enableProbeOptimization: false,
				meshMode:                netcfg.MeshCompatibilityModeDisabled,
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1", "10.10.1.3"),
				noop:      true,
				notMesh:   false,
				success:   true,
				numProbes: 0,
			},
		},
		{
			name: "mesh probe error",
			input: input{
				current: dests{
					ready:    sets.New("10.10.1.1"),
					notReady: sets.New("10.10.1.3"),
				},
				healthy:                 sets.New("10.10.1.1"),
				enableProbeOptimization: true,
				meshMode:                netcfg.MeshCompatibilityModeAuto,
				hostResponses: map[string][]activatortest.FakeResponse{
					"10.10.1.3": {{
						Code: http.StatusServiceUnavailable,
					}},
				},
			},
			expected: expected{
				healthy:   sets.New("10.10.1.1"),
				noop:      true,
				notMesh:   false,
				success:   false,
				numProbes: 1,
			},
		},
	}

	// Helper function to run the test and validate the results
	testFunc := func(testName string, input input, expected expected) {
		fakeRT := activatortest.FakeRoundTripper{
			ExpectHost: testRevision,
			ProbeResponses: []activatortest.FakeResponse{{
				Code: http.StatusOK,
				Body: queue.Name,
			}},
			ProbeHostResponses: input.hostResponses,
		}

		// Minimally constructed revisionWatcher just to have what is needed for probing
		rw := &revisionWatcher{
			rev:                     types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			logger:                  TestLogger(t),
			transport:               pkgnetwork.RoundTripperFunc(fakeRT.RT),
			enableProbeOptimisation: input.enableProbeOptimization,
			meshMode:                input.meshMode,
			healthyPods:             input.healthy,
		}

		healthy, noop, notMesh, err := rw.probePodIPs(input.current.ready, input.current.notReady)
		if !healthy.Equal(expected.healthy) {
			t.Errorf("%s: Healthy does not match, got %v, want %v diff: %s",
				testName, healthy, expected.healthy, cmp.Diff(healthy, expected.healthy))
		}
		if noop != expected.noop {
			t.Errorf("%s: Unexpected value for noop, got %t, want %t", testName, noop, expected.noop)
		}
		if notMesh != expected.notMesh {
			t.Errorf("%s: Unexpected value for notMesh, got %t, want %t",
				testName, notMesh, expected.notMesh)
		}
		if err != nil && expected.success {
			t.Errorf("%s: Unexpected error, got %v, want nil", testName, err)
		} else if err == nil && !expected.success {
			t.Errorf("%s: Unexpected error, got %v, want non-nil", testName, err)
		}
		if numProbes := fakeRT.NumProbes.Load(); numProbes != expected.numProbes {
			t.Errorf("%s: Unexpected number of probes, got %d, want %d",
				testName, numProbes, expected.numProbes)
		}
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testFunc(t.Name(), test.input, test.expected)
		})
	}
}
