/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package serverlessservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	// Inject the fakes for informers this reconciler depends on.
	networkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable"
	_ "knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable/fake"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgotesting "k8s.io/client-go/testing"

	"knative.dev/networking/pkg/apis/networking"
	nv1a1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	sksreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/serverlessservice"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources"
	presources "knative.dev/serving/pkg/resources"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing"
)

func TestNewController(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	c := NewController(ctx, configmap.NewStaticWatcher())
	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func TestReconcile(t *testing.T) {
	retryAttempted := false
	table := TableTest{{
		Name: "bad workqueue key, Part I",
		Key:  "too/many/parts",
	}, {
		Name: "bad workqueue key, Part II",
		Key:  "too-few-parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "steady state",
		Key:  "steady/state",
		Objects: []runtime.Object{
			SKS("steady", "state", markHappy, WithPubService, WithPrivateService, WithDeployRef("bar")),
			deploy("steady", "bar"),
			svcpub("steady", "state"),
			svcpriv("steady", "state"),
			endpointspub("steady", "state", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("steady", "state", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
	}, {
		// This is the case for once we are scaled to zero.
		// It also exercises the retry logic.
		Name: "steady switch to proxy mode, with retry",
		Key:  "steady/to-proxy",
		Objects: []runtime.Object{
			SKS("steady", "to-proxy", markHappy, WithPubService, WithPrivateService,
				WithDeployRef("bar"), withProxyMode),
			deploy("steady", "bar"),
			svcpub("steady", "to-proxy"),
			svcpriv("steady", "to-proxy"),
			endpointspub("steady", "to-proxy", withOtherSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("steady", "to-proxy"),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
				if retryAttempted || !action.Matches("update", "serverlessservices") || action.GetSubresource() != "status" {
					return false, nil, nil
				}
				retryAttempted = true
				return true, nil, apierrs.NewConflict(v1.Resource("foo"), "bar", errors.New("foo"))
			},
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("steady", "to-proxy", WithDeployRef("bar"), markNoEndpoints,
				withProxyMode, WithPubService, WithPrivateService),
		}, {
			Object: SKS("steady", "to-proxy", WithDeployRef("bar"), markNoEndpoints,
				withProxyMode, WithPubService, WithPrivateService),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("steady", "to-proxy", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		}},
	}, {
		Name: "steady switch to proxy mode, subset",
		Key:  "steady/to-proxy-with-subset",
		Objects: []runtime.Object{
			SKS("steady", "to-proxy-with-subset", markHappy, WithPubService, WithPrivateService,
				WithDeployRef("bar"), withProxyMode, WithNumActivators(5)),
			deploy("steady", "bar"),
			svcpub("steady", "to-proxy-with-subset"),
			svcpriv("steady", "to-proxy-with-subset"),
			endpointspub("steady", "to-proxy-with-subset", withOtherSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("steady", "to-proxy-with-subset"),
			activatorEndpoints(withNSubsets(2, 4 /*8 in total*/)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("steady", "to-proxy-with-subset", WithDeployRef("bar"), markNoEndpoints,
				withProxyMode, WithPubService, WithPrivateService, WithNumActivators(5)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("steady", "to-proxy-with-subset",
				withPickedSubset(2, 4, 5, "to-proxy-with-subset"),
				withFilteredPorts(networking.BackendHTTPPort)),
		}},
	}, {
		Name: "steady switch to proxy mode, subset all",
		Key:  "steady/to-proxy-with-subset",
		Objects: []runtime.Object{
			SKS("steady", "to-proxy-with-subset", markHappy, WithPubService, WithPrivateService,
				WithDeployRef("bar"), withProxyMode, WithNumActivators(8)),
			deploy("steady", "bar"),
			svcpub("steady", "to-proxy-with-subset"),
			svcpriv("steady", "to-proxy-with-subset"),
			endpointspub("steady", "to-proxy-with-subset", withOtherSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("steady", "to-proxy-with-subset"),
			activatorEndpoints(withNSubsets(2, 4 /*8 in total*/)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("steady", "to-proxy-with-subset", WithDeployRef("bar"), markNoEndpoints,
				withProxyMode, WithPubService, WithPrivateService, WithNumActivators(8)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("steady", "to-proxy-with-subset",
				withPickedSubset(2, 4, 8, "to-proxy-with-subset"),
				withFilteredPorts(networking.BackendHTTPPort)),
		}},
	}, {
		// This is the case for once we are proxying for unsufficient burst capacity.
		// It should be a no-op.
		Name: "steady switch to proxy mode with endpoints",
		Key:  "steady/to-proxy",
		Objects: []runtime.Object{
			SKS("steady", "to-proxy", markHappy, WithPubService, WithPrivateService,
				WithDeployRef("bar"), withProxyMode),
			deploy("steady", "bar"),
			svcpub("steady", "to-proxy"),
			svcpriv("steady", "to-proxy"),
			endpointspub("steady", "to-proxy", withOtherSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("steady", "to-proxy", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("steady", "to-proxy", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		}},
	}, {
		Name: "user changes public svc",
		Key:  "public/svc-change",
		Objects: []runtime.Object{
			SKS("public", "svc-change", WithPubService, WithSKSReady,
				WithPrivateService, WithDeployRef("bar")),
			deploy("public", "bar"),
			svcpub("public", "svc-change", withTimeSelector),
			svcpriv("public", "svc-change"),
			endpointspub("public", "svc-change", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("public", "svc-change", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpub("public", "svc-change"),
		}},
	}, {
		Name: "user changes priv svc",
		Key:  "private/svc-change",
		Objects: []runtime.Object{
			SKS("private", "svc-change", markHappy, WithPubService,
				WithPrivateService, WithDeployRef("baz")),
			deploy("private", "baz"),
			svcpub("private", "svc-change"),
			svcpriv("private", "svc-change", withTimeSelector),
			endpointspub("private", "svc-change", withOtherSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("private", "svc-change", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpriv("private", "svc-change"),
		}, {
			Object: endpointspub("private", "svc-change", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		}},
	}, {
		Name: "OnCreate-deployment-does-not-exist",
		Key:  "on/cde",
		Objects: []runtime.Object{
			SKS("on", "cde", WithDeployRef("blah"), markNoEndpoints),
			deploy("on", "blah-another"),
			endpointspriv("on", "cde", WithSubsets),
		},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `error retrieving deployment selector spec: error fetching Pod Scalable on/blah: deployments.apps "blah" not found`),
		},
	}, {
		Name: "OnCreate-deployment-exists",
		Key:  "on/cde",
		Objects: []runtime.Object{
			SKS("on", "cde", WithDeployRef("blah")),
			deploy("on", "blah"),
			// This "has" to pre-exist, otherwise I can't populate it with subsets.
			endpointspriv("on", "cde", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cde"),
			svcpub("on", "cde"),
			endpointspub("on", "cde", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cde", WithDeployRef("blah"),
				markHappy, WithPubService, WithPrivateService),
		}},
	}, {
		Name:    "update-eps-fail",
		Key:     "update-eps/failA",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("update-eps", "failA", WithPubService, WithPrivateService, WithDeployRef("blah"), markNoEndpoints),
			deploy("update-eps", "blah"),
			svcpub("update-eps", "failA"),
			svcpriv("update-eps", "failA"),
			endpointspub("update-eps", "failA"),
			endpointspriv("update-eps", "failA", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "endpoints"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("update-eps", "failA", WithSubsets,
				withFilteredPorts(networking.BackendHTTPPort)), // The attempted update.
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to update public K8s Endpoints: inducing failure for update endpoints"),
		},
	}, {
		Name:    "svc-fail-pub",
		Key:     "svc/fail2",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("svc", "fail2", WithDeployRef("blah")),
			deploy("svc", "blah"),
			svcpriv("svc", "fail2"),
			endpointspriv("svc", "fail2"),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("svc", "fail2", WithPrivateService,
				WithDeployRef("blah"), markTransitioning("CreatingPublicService")),
		}},
		WantCreates: []runtime.Object{
			svcpub("svc", "fail2"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to create public K8s Service: inducing failure for create services"),
		},
	}, {
		Name:    "eps-create-fail-pub",
		Key:     "eps/fail3",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("eps", "fail3", WithDeployRef("blah")),
			deploy("eps", "blah"),
			svcpriv("eps", "fail3"),
			endpointspriv("eps", "fail3", WithSubsets),
			activatorEndpoints(withOtherSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "endpoints"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("eps", "fail3", WithPubService, WithPrivateService,
				WithDeployRef("blah"), markTransitioning("CreatingPublicEndpoints")),
		}},
		WantCreates: []runtime.Object{
			svcpub("eps", "fail3"),
			endpointspub("eps", "fail3", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create public K8s Endpoints: inducing failure for create endpoints"),
		},
	}, {
		Name: "OnCreate-no-eps",
		Key:  "on/cneps",
		Objects: []runtime.Object{
			SKS("on", "cneps", WithDeployRef("blah"), WithPrivateService),
			deploy("on", "blah"),
			endpointspriv("on", "cneps"),
			activatorEndpoints(WithSubsets),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cneps"),
			svcpub("on", "cneps"),
			endpointspub("on", "cneps", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cneps", WithDeployRef("blah"),
				markNoEndpoints, WithPubService, WithPrivateService),
		}},
	}, {
		Name: "OnCreate-no-activator-eps-exist",
		Key:  "on/cnaeps2",
		Objects: []runtime.Object{
			SKS("on", "cnaeps2", WithDeployRef("blah")),
			deploy("on", "blah"),
			endpointspriv("on", "cnaeps2", WithSubsets),
			endpointspub("on", "cnaeps2", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		},
		WantErr: true,
		WantCreates: []runtime.Object{
			svcpriv("on", "cnaeps2"),
			svcpub("on", "cnaeps2"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cnaeps2", WithDeployRef("blah"), WithPubService,
				WithPrivateService,
				markTransitioning("CreatingPublicService")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to get activator service endpoints: endpoints "activator-service" not found`),
		},
	}, {
		Name: "OnCreate-no-private-eps-exist",
		Key:  "on/cnaeps3",
		Objects: []runtime.Object{
			SKS("on", "cnaeps3", WithDeployRef("blah")),
			deploy("on", "blah"),
			endpointspub("on", "cnaeps3", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantErr: true,
		WantCreates: []runtime.Object{
			svcpriv("on", "cnaeps3"),
			svcpub("on", "cnaeps3"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cnaeps3", WithDeployRef("blah"), WithPubService,
				WithPrivateService,
				markTransitioning("CreatingPublicService")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to get private K8s Service endpoints: endpoints "cnaeps3-private" not found`),
		},
	}, {
		Name: "OnCreate-no-activator-eps-service",
		Key:  "on/cnaeps",
		Objects: []runtime.Object{
			SKS("on", "cnaeps", WithDeployRef("blah")),
			deploy("on", "blah"),
			endpointspriv("on", "cnaeps", WithSubsets),
			endpointspub("on", "cnaeps", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			activatorEndpoints(),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cnaeps"),
			svcpub("on", "cnaeps"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cnaeps", WithDeployRef("blah"),
				markHappy, WithPubService, WithPrivateService),
		}},
	}, {
		Name: "OnCreate-no-activator-eps-proxy",
		Key:  "on/cnaeps",
		Objects: []runtime.Object{
			SKS("on", "cnaeps", WithDeployRef("blah"), withProxyMode),
			deploy("on", "blah"),
			endpointspriv("on", "cnaeps"), // This should be ignored.
			activatorEndpoints(),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cnaeps"),
			svcpub("on", "cnaeps", withTargetPortNum(8012)),
			endpointspub("on", "cnaeps"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cnaeps", WithDeployRef("blah"), withProxyMode,
				markNoEndpoints, WithPubService, WithPrivateService),
		}},
	}, {
		Name:    "create-svc-fail-priv",
		Key:     "svc/fail",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("svc", "fail", WithDeployRef("blah")),
			deploy("svc", "blah"),
			endpointspriv("svc", "fail"),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("svc", "fail", WithDeployRef("blah"), markTransitioning("CreatingPrivateService")),
		}},
		WantCreates: []runtime.Object{
			svcpriv("svc", "fail"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to create private K8s Service: inducing failure for create services"),
		},
	}, {
		Name:    "update-sks-fail",
		Key:     "update-sks/fail4",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("update-sks", "fail4", WithPubService, WithPrivateService,
				WithDeployRef("blah")),
			deploy("update-sks", "blah"),
			svcpub("update-sks", "fail4"),
			svcpriv("update-sks", "fail4"),
			endpointspub("update-sks", "fail4", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
			endpointspriv("update-sks", "fail4", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "serverlessservices"),
		},
		// We still record update, but it fails.
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("update-sks", "fail4",
				WithDeployRef("blah"), markHappy, WithPubService, WithPrivateService),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `Failed to update status for "fail4": inducing failure for update serverlessservices`),
		},
	}, {
		Name:    "ronin-priv-service",
		Key:     "ronin-priv-service/fail5",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("ronin-priv-service", "fail5", WithPubService, WithPrivateService,
				WithDeployRef("blah"), markHappy),
			deploy("ronin-priv-service", "blah"),
			svcpub("ronin-priv-service", "fail5"),
			svcpriv("ronin-priv-service", "fail5", WithK8sSvcOwnersRemoved),
			endpointspub("ronin-priv-service", "fail5", WithSubsets),
			endpointspriv("ronin-priv-service", "fail5", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("ronin-priv-service", "fail5", WithPubService, WithPrivateService,
				WithDeployRef("blah"), markUnowned("Service", "fail5-private")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `SKS: fail5 does not own Service: fail5-private`),
		},
	}, {
		Name:    "ronin-pub-service",
		Key:     "ronin-pub-service/fail6",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("ronin-pub-service", "fail6", WithPubService, WithPrivateService,
				WithDeployRef("blah")),
			deploy("ronin-pub-service", "blah"),
			svcpub("ronin-pub-service", "fail6", WithK8sSvcOwnersRemoved),
			svcpriv("ronin-pub-service", "fail6"),
			endpointspub("ronin-pub-service", "fail6", WithSubsets),
			endpointspriv("ronin-pub-service", "fail6", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("ronin-pub-service", "fail6", WithPubService, WithPrivateService,
				WithDeployRef("blah"), markUnowned("Service", "fail6")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `SKS: fail6 does not own Service: fail6`),
		},
	}, {
		Name:    "ronin-pub-eps",
		Key:     "ronin-pub-eps/fail7",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("ronin-pub-eps", "fail7", WithPubService, WithPrivateService,
				WithDeployRef("blah")),
			deploy("ronin-pub-eps", "blah"),
			svcpub("ronin-pub-eps", "fail7"),
			svcpriv("ronin-pub-eps", "fail7"),
			endpointspub("ronin-pub-eps", "fail7", WithSubsets, WithEndpointsOwnersRemoved),
			endpointspriv("ronin-pub-eps", "fail7", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("ronin-pub-eps", "fail7", WithPubService, WithPrivateService,
				WithDeployRef("blah"), markUnowned("Endpoints", "fail7")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `SKS: fail7 does not own Endpoints: fail7`),
		},
	}, {
		Name:    "update-priv-svc-fail",
		Key:     "update-svc/fail9",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("update-svc", "fail9", WithPubService, WithPrivateService,
				WithDeployRef("blah")),
			deploy("update-svc", "blah"),
			svcpub("update-svc", "fail9"),
			svcpriv("update-svc", "fail9", withTimeSelector),
			endpointspub("update-svc", "fail9"),
			endpointspriv("update-svc", "fail9"),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("update-svc", "fail9", WithPubService, WithPrivateService,
				WithDeployRef("blah"), markTransitioning("UpdatingPrivateService")),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpriv("update-svc", "fail9"),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to update private K8s Service: inducing failure for update services"),
		},
	}, {
		Name:    "update-pub-svc-fail",
		Key:     "update-svc/fail8",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("update-svc", "fail8", WithPubService, WithDeployRef("blah"), markHappy, WithPrivateService),
			deploy("update-svc", "blah"),
			svcpub("update-svc", "fail8", withTimeSelector),
			svcpriv("update-svc", "fail8"),
			endpointspub("update-svc", "fail8", WithSubsets),
			endpointspriv("update-svc", "fail8", WithSubsets),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpub("update-svc", "fail8"),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update public K8s Service: inducing failure for update services"),
		},
	}, {
		Name: "pod change",
		Key:  "pod/change",
		Objects: []runtime.Object{
			SKS("pod", "change", markHappy, WithPubService, WithPrivateService,
				WithDeployRef("blah")),
			deploy("pod", "blah"),
			svcpub("pod", "change"),
			svcpriv("pod", "change"),
			endpointspub("pod", "change", WithSubsets),
			endpointspriv("pod", "change", withOtherSubsets),
			activatorEndpoints(WithSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("pod", "change", withOtherSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		}},
	}, {
		Name: "proxy mode; pod change - activator",
		Key:  "pod/change",
		Objects: []runtime.Object{
			SKS("pod", "change", markNoEndpoints, WithPubService, withHTTP2Protocol,
				WithPrivateService, WithDeployRef("blah")),
			deploy("pod", "blah"),
			svcpub("pod", "change", withHTTP2),
			svcpriv("pod", "change", withHTTP2Priv),
			endpointspub("pod", "change", WithSubsets),
			endpointspriv("pod", "change"),
			activatorEndpoints(withOtherSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("pod", "change", withOtherSubsets, withFilteredPorts(networking.BackendHTTP2Port)),
		}},
	}, {
		Name: "serving mode; serving pod comes online",
		Key:  "pod/change",
		Objects: []runtime.Object{
			SKS("pod", "change", markNoEndpoints, WithPubService,
				WithPrivateService, WithDeployRef("blah")),
			deploy("pod", "blah"),
			svcpub("pod", "change"),
			svcpriv("pod", "change"),
			endpointspub("pod", "change", withOtherSubsets),
			endpointspriv("pod", "change", WithSubsets),
			activatorEndpoints(withOtherSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("pod", "change",
				WithDeployRef("blah"), markHappy, WithPubService, WithPrivateService, WithDeployRef("blah")),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("pod", "change", WithSubsets, withFilteredPorts(networking.BackendHTTPPort)),
		}},
	}, {
		Name: "serving mode; no backend endpoints",
		Key:  "pod/change",
		Objects: []runtime.Object{
			SKS("pod", "change", WithSKSReady, WithPubService, withHTTP2Protocol,
				WithPrivateService, WithDeployRef("blah")),
			deploy("pod", "blah"),
			svcpub("pod", "change", withHTTP2),
			svcpriv("pod", "change", withHTTP2Priv),
			endpointspub("pod", "change", WithSubsets), // We had endpoints...
			endpointspriv("pod", "change"),             // but now we don't.
			activatorEndpoints(withOtherSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("pod", "change", withHTTP2Protocol,
				WithDeployRef("blah"), markNoEndpoints, WithPubService, WithPrivateService),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("pod", "change", withOtherSubsets, withFilteredPorts(networking.BackendHTTP2Port)),
		}},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		retryAttempted = false
		ctx = podscalable.WithDuck(ctx)

		r := &reconciler{
			kubeclient:        kubeclient.Get(ctx),
			serviceLister:     listers.GetK8sServiceLister(),
			endpointsLister:   listers.GetEndpointsLister(),
			psInformerFactory: podscalable.Get(ctx),
		}

		return sksreconciler.NewReconciler(ctx, logging.FromContext(ctx), networkingclient.Get(ctx),
			listers.GetServerlessServiceLister(), controller.GetEventRecorder(ctx), r)
	}))
}

// Keeps only desired port.
func withFilteredPorts(port int32) EndpointsOption {
	return func(ep *corev1.Endpoints) {
		for i := range ep.Subsets {
			for _, p := range ep.Subsets[i].Ports {
				if p.Port == port {
					ep.Subsets[i].Ports[i] = p
					break
				}
			}
			// Strip all the others.
			ep.Subsets[i].Ports = ep.Subsets[i].Ports[:1]
		}
	}
}

// withPickedSubset simulates the picking of the activator
// address subset.
func withPickedSubset(numSS, numAddrs, pickN int, target string) EndpointsOption {
	return func(ep *corev1.Endpoints) {
		// Generate the full set.
		withNSubsets(numSS, numAddrs)(ep)
		// Now pick and replace.
		ep.Subsets = subsetEndpoints(ep, target, pickN).Subsets
	}
}

// withOtherSubsets uses different IP set than functional::withSubsets.
func withOtherSubsets(ep *corev1.Endpoints) {
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{IP: "127.0.0.2"}},
		Ports:     []corev1.EndpointPort{{Port: 8013}, {Port: 8012}},
	}}
}

func markHappy(sks *nv1a1.ServerlessService) {
	sks.Status.MarkEndpointsReady()
}

func markUnowned(k, n string) SKSOption {
	return func(sks *nv1a1.ServerlessService) {
		sks.Status.MarkEndpointsNotOwned(k, n)
	}
}

func markTransitioning(s string) SKSOption {
	return func(sks *nv1a1.ServerlessService) {
		sks.Status.MarkEndpointsNotReady(s)
	}
}

func markNoEndpoints(sks *nv1a1.ServerlessService) {
	sks.Status.MarkEndpointsNotReady("NoHealthyBackends")
	sks.Status.MarkActivatorEndpointsPopulated()
}

func withHTTP2Protocol(sks *nv1a1.ServerlessService) {
	sks.Spec.ProtocolType = networking.ProtocolH2C
}

type deploymentOption func(*appsv1.Deployment)

func deploy(namespace, name string, opts ...deploymentOption) *appsv1.Deployment {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"label": "value",
				},
			},
			Replicas: ptr.Int32(1),
		},
	}

	for _, opt := range opts {
		opt(d)
	}
	return d
}

func withHTTP2Priv(svc *corev1.Service) {
	svc.Spec.Ports[0].Name = "http2"
	svc.Spec.Ports[0].TargetPort = intstr.FromInt(networking.BackendHTTP2Port)
}

func withHTTP2(svc *corev1.Service) {
	svc.Spec.Ports[0].Port = networking.ServiceHTTP2Port
	svc.Spec.Ports[0].Name = "http2"
	svc.Spec.Ports[0].TargetPort = intstr.FromInt(networking.BackendHTTP2Port)
}

// For SKS internal tests this sets mode & activator status.
func withProxyMode(sks *nv1a1.ServerlessService) {
	WithProxyMode(sks)
	sks.Status.MarkActivatorEndpointsPopulated()
}

func withTargetPortNum(port int) K8sServiceOption {
	return func(svc *corev1.Service) {
		svc.Spec.Ports[0].TargetPort = intstr.FromInt(port)
	}
}

func svcpub(namespace, name string, so ...K8sServiceOption) *corev1.Service {
	sks := SKS(namespace, name)
	s := resources.MakePublicService(sks)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func svcpriv(namespace, name string, so ...K8sServiceOption) *corev1.Service {
	sks := SKS(namespace, name)
	s := resources.MakePrivateService(sks, map[string]string{
		"label": "value",
	})
	for _, opt := range so {
		opt(s)
	}
	return s
}

func activatorEndpoints(eo ...EndpointsOption) *corev1.Endpoints {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      networking.ActivatorServiceName,
		},
	}
	for _, opt := range eo {
		opt(ep)
	}
	return ep
}

func endpointspriv(namespace, name string, eo ...EndpointsOption) *corev1.Endpoints {
	service := svcpriv(namespace, name)
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
	for _, opt := range eo {
		opt(ep)
	}
	return ep
}

// withNSubsets populates the endpoints object with numSS subsets
// each having numAddr endpoints.
func withNSubsets(numSS, numAddr int) EndpointsOption {
	return func(ep *corev1.Endpoints) {
		ep.Subsets = make([]corev1.EndpointSubset, numSS)
		for i := 0; i < numSS; i++ {
			ep.Subsets[i].Ports = []corev1.EndpointPort{{Port: 8012}, {Port: 8013}}
			ep.Subsets[i].Addresses = make([]corev1.EndpointAddress, numAddr)
			for j := 0; j < numAddr; j++ {
				ep.Subsets[i].Addresses[j].IP = fmt.Sprintf("10.1.%d.%d", i+1, j+1)
			}
		}
	}
}

func endpointspub(namespace, name string, eo ...EndpointsOption) *corev1.Endpoints {
	service := svcpub(namespace, name)
	ep := &corev1.Endpoints{
		ObjectMeta: *service.ObjectMeta.DeepCopy(),
	}
	for _, opt := range eo {
		opt(ep)
	}
	return ep
}

func withTimeSelector(svc *corev1.Service) {
	svc.Spec.Selector = map[string]string{"pod-x": fmt.Sprintf("a-%d", time.Now().UnixNano())}
}

func TestSubsetEndpoints(t *testing.T) {
	// This just tests the `subsetEndpoints` helper.
	t.Run("empty", func(t *testing.T) {
		aeps := activatorEndpoints()
		if got, want := subsetEndpoints(aeps, "rev", 1), aeps; got != want {
			t.Errorf("Empty EPS = %p, want: %p", got, want)
		}
		aeps = activatorEndpoints(withNSubsets(1, 0))
		if got, want := subsetEndpoints(aeps, "rev", 1), aeps; got != want {
			t.Errorf("Empty EPS = %p, want: %p", got, want)
		}
	})
	t.Run("over-requested or all", func(t *testing.T) {
		tests := []struct {
			name            string
			nss, naddr, req int
		}{{
			"1x1", 1, 1, 1,
		}, {
			"1x2", 1, 2, 2,
		}, {
			"2x1", 2, 1, 3,
		}, {
			"20x10", 20, 10, 212,
		}, {
			"20x10", 20, 10, 0,
		}}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				aeps := activatorEndpoints(withNSubsets(tc.nss, tc.naddr))
				if got, want := subsetEndpoints(aeps, "rev", tc.req), aeps; got != want {
					t.Errorf("Select all: EPS = %p, want: %p", got, want)
				}
			})
		}
	})
	t.Run("actual subset", func(t *testing.T) {
		// We need to verify two things
		// 1. that exacly N items were returned
		// 2. they are distinct
		// 3. No empty subset is returned.
		tests := []struct {
			name            string
			nss, naddr, req int
		}{{
			"1x2 - 1", 1, 2, 1,
		}, {
			"2x1 - 1", 2, 1, 1,
		}, {
			"5x5 - 1", 5, 5, 1,
		}, {
			"5x5 - 12", 5, 5, 12,
		}, {
			"5x5 - 24", 5, 5, 24,
		}}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				aeps := activatorEndpoints(withNSubsets(tc.nss, tc.naddr))
				subset := subsetEndpoints(aeps, "target", tc.req)
				if got, want := presources.ReadyAddressCount(subset), tc.req; got != want {
					t.Errorf("Endpoint count = %d, want: %d", got, want)
				}
				for i, ss := range subset.Subsets {
					if len(ss.Addresses) == 0 {
						t.Errorf("Size of subset %d is 0", i)
					}
				}
			})
		}
	})
}
