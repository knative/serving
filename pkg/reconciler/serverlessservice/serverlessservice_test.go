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
	"fmt"
	"testing"
	"time"

	// Inject the fakes for informers this reconciler depends on.
	_ "github.com/knative/pkg/injection/informers/kubeinformers/corev1/endpoints/fake"
	_ "github.com/knative/pkg/injection/informers/kubeinformers/corev1/service/fake"
	_ "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/ptr"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/networking"
	nv1a1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	rpkg "github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/serverlessservice/resources"
	presources "github.com/knative/serving/pkg/resources"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/pkg/reconciler/testing"
	. "github.com/knative/serving/pkg/reconciler/testing/v1alpha1"
	. "github.com/knative/serving/pkg/testing"
)

func TestNewController(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)
	c := NewController(ctx, configmap.NewStaticWatcher())
	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name:                    "bad workqueue key, Part I",
		Key:                     "too/many/parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "bad workqueue key, Part II",
		Key:                     "too-few-parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "key not found",
		Key:                     "foo/not-found",
		SkipNamespaceValidation: true,
	}, {
		Name: "steady state",
		Key:  "steady/state",
		Objects: []runtime.Object{
			SKS("steady", "state", markHappy, WithPubService, WithPrivateService("state-fsdf"),
				WithDeployRef("bar")),
			deploy("steady", "bar"),
			svcpub("steady", "state"),
			svcpriv("steady", "state", svcWithName("state-fsdf")),
			endpointspub("steady", "state", WithSubsets),
			endpointspriv("steady", "state", WithSubsets, epsWithName("state-fsdf")),
			activatorEndpoints(WithSubsets),
		},
	}, {
		Name: "steady switch to proxy mode",
		Key:  "steady/to-proxy",
		Objects: []runtime.Object{
			SKS("steady", "to-proxy", markHappy, WithPubService, WithPrivateService("to-proxy-deadbeef"),
				WithDeployRef("bar"), WithProxyMode),
			deploy("steady", "bar"),
			svcpub("steady", "to-proxy"),
			svcpriv("steady", "to-proxy", svcWithName("to-proxy-deadbeef")),
			endpointspub("steady", "to-proxy", withOtherSubsets),
			endpointspriv("steady", "to-proxy", epsWithName("to-proxy-deadbeed")),
			activatorEndpoints(WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("steady", "to-proxy", WithDeployRef("bar"),
				markNoEndpoints, WithProxyMode, WithPubService, WithPrivateService("to-proxy-deadbeef")),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("steady", "to-proxy", WithSubsets),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "steady/to-proxy"`),
		},
	}, {
		Name: "many-private-services",
		Key:  "many/privates",
		Objects: []runtime.Object{
			SKS("many", "privates", markHappy, WithPubService, WithPrivateService("privates-elegance-required"),
				WithDeployRef("bar")),
			deploy("many", "bar"),
			svcpub("many", "privates"),
			svcpriv("many", "privates", svcWithName("privates-elegance-required")),
			svcpriv("many", "privates", svcWithName("privates-brutality-is-here")),
			svcpriv("many", "privates", svcWithName("privates-uncharacteristically-pretty"),
				WithK8sSvcOwnersRemoved), // unowned, should remain.
			endpointspub("many", "privates", WithSubsets),
			endpointspriv("many", "privates", WithSubsets, epsWithName("privates-elegance-required")),
			activatorEndpoints(WithSubsets),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "many",
				Verb:      "delete",
				Resource: schema.GroupVersionResource{
					Group:    "core",
					Version:  "v1",
					Resource: "services",
				},
			},
			Name: "privates-brutality-is-here",
		}},
	}, {
		Name: "user changes public svc",
		Key:  "public/svc-change",
		Objects: []runtime.Object{
			SKS("public", "svc-change", WithPubService, WithSKSReady,
				WithPrivateService("svc-change-feedbeef"), WithDeployRef("bar")),
			deploy("public", "bar"),
			svcpub("public", "svc-change", withTimeSelector),
			svcpriv("public", "svc-change", svcWithName("svc-change-feedbeef")),
			endpointspub("public", "svc-change", WithSubsets),
			endpointspriv("public", "svc-change", WithSubsets, epsWithName("svc-change-feedbeef")),
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
				WithPrivateService("svc-change-fade"), WithDeployRef("baz")),
			deploy("private", "baz"),
			svcpub("private", "svc-change"),
			svcpriv("private", "svc-change", withTimeSelector, svcWithName("svc-change-fade")),
			endpointspub("private", "svc-change", withOtherSubsets),
			endpointspriv("private", "svc-change", WithSubsets, epsWithName("svc-change-fade")),
			activatorEndpoints(WithSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpriv("private", "svc-change", svcWithName("svc-change-fade")),
		}, {
			Object: endpointspub("private", "svc-change", WithSubsets),
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
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `InternalError: error retrieving deployment selector spec: error fetching Pod Scalable on/blah: deployments.apps "blah" not found`),
		},
	}, {
		Name: "OnCreate-deployment-exists",
		Key:  "on/cde",
		Objects: []runtime.Object{
			SKS("on", "cde", WithDeployRef("blah")),
			deploy("on", "blah"),
			// This "has" to pre-exist, otherwise I can't populate it with subsets.
			endpointspriv("on", "cde", WithSubsets, epsWithName("cde-00001")),
			activatorEndpoints(WithSubsets),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cde"),
			svcpub("on", "cde"),
			endpointspub("on", "cde", WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cde", WithDeployRef("blah"),
				markHappy, WithPubService, WithPrivateService("cde-00001")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "on/cde"`),
		},
	}, {
		Name:    "update-eps-fail",
		Key:     "update-eps/failA",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("update-eps", "failA", WithPubService, WithPrivateService("failA-abba"), WithDeployRef("blah"), markNoEndpoints),
			deploy("update-eps", "blah"),
			svcpub("update-eps", "failA"),
			svcpriv("update-eps", "failA", svcWithName("failA-abba")),
			endpointspub("update-eps", "failA"),
			endpointspriv("update-eps", "failA", WithSubsets, epsWithName("failA-abba")),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "endpoints"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("update-eps", "failA", WithSubsets), // The attempted update.
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for update endpoints"),
		},
	}, {
		Name:    "svc-fail-pub",
		Key:     "svc/fail2",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("svc", "fail2", WithDeployRef("blah")),
			deploy("svc", "blah"),
			svcpriv("svc", "fail2", svcWithName("fail2-badbeef")),
			endpointspriv("svc", "fail2"),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("svc", "fail2", WithPrivateService("fail2-badbeef"), WithDeployRef("blah"), markTransitioning("CreatingPublicService")),
		}},
		WantCreates: []runtime.Object{
			svcpub("svc", "fail2"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for create services"),
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "svc/fail2"`),
		},
	}, {
		Name:    "eps-create-fail-pub",
		Key:     "eps/fail3",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("eps", "fail3", WithDeployRef("blah")),
			deploy("eps", "blah"),
			svcpriv("eps", "fail3", svcWithName("fail3-abbad")),
			endpointspriv("eps", "fail3", WithSubsets, epsWithName("fail3-abbad")),
			activatorEndpoints(withOtherSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "endpoints"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("eps", "fail3", WithPubService, WithPrivateService("fail3-abbad"), WithDeployRef("blah"),
				markTransitioning("CreatingPublicEndpoints")),
		}},
		WantCreates: []runtime.Object{
			svcpub("eps", "fail3"),
			endpointspub("eps", "fail3", WithSubsets),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for create endpoints"),
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "eps/fail3"`),
		},
	}, {
		Name: "OnCreate-no-eps",
		Key:  "on/cneps",
		Objects: []runtime.Object{
			SKS("on", "cneps", WithDeployRef("blah"), WithPrivateService("cneps-incorrect")),
			deploy("on", "blah"),
			endpointspriv("on", "cneps", epsWithName("cneps-00001")),
			activatorEndpoints(WithSubsets),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cneps"),
			svcpub("on", "cneps"),
			endpointspub("on", "cneps", WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cneps", WithDeployRef("blah"),
				markNoEndpoints, WithPubService, WithPrivateService("cneps-00001")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "on/cneps"`),
		},
	}, {
		Name: "OnCreate-no-activator-eps-serve",
		Key:  "on/cnaeps",
		Objects: []runtime.Object{
			SKS("on", "cnaeps", WithDeployRef("blah")),
			deploy("on", "blah"),
			endpointspriv("on", "cnaeps", WithSubsets, epsWithName("cnaeps-00001")),
			endpointspub("on", "cnaeps", WithSubsets),
			activatorEndpoints(),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cnaeps"),
			svcpub("on", "cnaeps"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cnaeps", WithDeployRef("blah"),
				markHappy, WithPubService, WithPrivateService("cnaeps-00001")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "on/cnaeps"`),
		},
	}, {
		Name: "OnCreate-no-activator-eps-proxy",
		Key:  "on/cnaeps",
		Objects: []runtime.Object{
			SKS("on", "cnaeps", WithDeployRef("blah"), WithProxyMode),
			deploy("on", "blah"),
			endpointspriv("on", "cnaeps", WithSubsets), // This should be ignored.
			activatorEndpoints(),
		},
		WantCreates: []runtime.Object{
			svcpriv("on", "cnaeps"),
			svcpub("on", "cnaeps", withTargetPortNum(8012)),
			endpointspub("on", "cnaeps"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("on", "cnaeps", WithDeployRef("blah"), WithProxyMode,
				markNoEndpoints, WithPubService, WithPrivateService("cnaeps-00001")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "on/cnaeps"`),
		},
	}, {
		Name:    "create-svc-fail-priv",
		Key:     "svc/fail",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("svc", "fail", WithDeployRef("blah")),
			deploy("svc", "blah"),
			endpointspriv("svc", "fail", epsWithName("blah-00001")),
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
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for create services"),
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "svc/fail"`),
		},
	}, {
		Name:    "update-sks-fail",
		Key:     "update-sks/fail4",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("update-sks", "fail4", WithPubService, WithPrivateService("fail4-42x"),
				WithDeployRef("blah")),
			deploy("update-sks", "blah"),
			svcpub("update-sks", "fail4"),
			svcpriv("update-sks", "fail4", svcWithName("fail4-42x")),
			endpointspub("update-sks", "fail4", WithSubsets),
			endpointspriv("update-sks", "fail4", WithSubsets, epsWithName("fail4-42x")),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "serverlessservices"),
		},
		// We still record update, but it fails.
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("update-sks", "fail4",
				WithDeployRef("blah"), markHappy, WithPubService, WithPrivateService("fail4-42x")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status: inducing failure for update serverlessservices"),
		},
	}, {
		Name:    "ronin-priv-service",
		Key:     "ronin-priv-service/fail5",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("ronin-priv-service", "fail5", WithPubService, WithPrivateService("fail5-fender"),
				WithDeployRef("blah"), markHappy),
			deploy("ronin-priv-service", "blah"),
			svcpub("ronin-priv-service", "fail5"),
			svcpriv("ronin-priv-service", "fail5", WithK8sSvcOwnersRemoved, svcWithName("fail5-fender")),
			endpointspub("ronin-priv-service", "fail5", WithSubsets),
			endpointspriv("ronin-priv-service", "fail5", WithSubsets, epsWithName("fail5-fender")),
			activatorEndpoints(WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("ronin-priv-service", "fail5", WithPubService, WithPrivateService("fail5-fender"),
				WithDeployRef("blah"), markUnowned("Service", "fail5-fender")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `InternalError: SKS: fail5 does not own Service: fail5-fender`),
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "ronin-priv-service/fail5"`),
		},
	}, {
		Name:    "ronin-pub-service",
		Key:     "ronin-pub-service/fail6",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("ronin-pub-service", "fail6", WithPubService, WithPrivateService("fail6-gibson"),
				WithDeployRef("blah")),
			deploy("ronin-pub-service", "blah"),
			svcpub("ronin-pub-service", "fail6", WithK8sSvcOwnersRemoved),
			svcpriv("ronin-pub-service", "fail6", svcWithName("fail6-gibson")),
			endpointspub("ronin-pub-service", "fail6", WithSubsets),
			endpointspriv("ronin-pub-service", "fail6", WithSubsets, epsWithName("fail6-gibson")),
			activatorEndpoints(WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("ronin-pub-service", "fail6", WithPubService, WithPrivateService("fail6-gibson"),
				WithDeployRef("blah"), markUnowned("Service", "fail6")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `InternalError: SKS: fail6 does not own Service: fail6`),
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "ronin-pub-service/fail6"`),
		},
	}, {
		Name:    "ronin-pub-eps",
		Key:     "ronin-pub-eps/fail7",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("ronin-pub-eps", "fail7", WithPubService, WithPrivateService("fail7-schecter"),
				WithDeployRef("blah")),
			deploy("ronin-pub-eps", "blah"),
			svcpub("ronin-pub-eps", "fail7"),
			svcpriv("ronin-pub-eps", "fail7", svcWithName("fail7-schecter")),
			endpointspub("ronin-pub-eps", "fail7", WithSubsets, WithEndpointsOwnersRemoved),
			endpointspriv("ronin-pub-eps", "fail7", WithSubsets, epsWithName("fail7-schecter")),
			activatorEndpoints(WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("ronin-pub-eps", "fail7", WithPubService, WithPrivateService("fail7-schecter"),
				WithDeployRef("blah"), markUnowned("Endpoints", "fail7")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `InternalError: SKS: fail7 does not own Endpoints: fail7`),
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "ronin-pub-eps/fail7"`),
		},
	}, {
		Name:    "update-priv-svc-fail",
		Key:     "update-svc/fail9",
		WantErr: true,
		Objects: []runtime.Object{
			SKS("update-svc", "fail9", WithPubService, WithPrivateService("fail9-yamaha"),
				WithDeployRef("blah")),
			deploy("update-svc", "blah"),
			svcpub("update-svc", "fail9"),
			svcpriv("update-svc", "fail9", withTimeSelector, svcWithName("fail9-yamaha")),
			endpointspub("update-svc", "fail9"),
			endpointspriv("update-svc", "fail9", epsWithName("fail9-yamaha")),
			activatorEndpoints(WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: SKS("update-svc", "fail9", WithPubService, WithPrivateService("fail9-yamaha"),
				WithDeployRef("blah"), markTransitioning("UpdatingPrivateService")),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpriv("update-svc", "fail9", svcWithName("fail9-yamaha")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for update services"),
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "update-svc/fail9"`),
		},
	},
		{
			Name:    "update-pub-svc-fail",
			Key:     "update-svc/fail8",
			WantErr: true,
			Objects: []runtime.Object{
				SKS("update-svc", "fail8", WithPubService, WithDeployRef("blah"), markHappy, WithPrivateService("fail8-ibanez")),
				deploy("update-svc", "blah"),
				svcpub("update-svc", "fail8", withTimeSelector),
				svcpriv("update-svc", "fail8", svcWithName("fail8-ibanez")),
				endpointspub("update-svc", "fail8", WithSubsets),
				endpointspriv("update-svc", "fail8", WithSubsets, epsWithName("fail8-ibanez")),
				activatorEndpoints(WithSubsets),
			},
			WithReactors: []clientgotesting.ReactionFunc{
				InduceFailure("update", "services"),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{
				Object: svcpub("update-svc", "fail8"),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for update services"),
			},
		}, {
			Name: "pod change",
			Key:  "pod/change",
			Objects: []runtime.Object{
				SKS("pod", "change", markHappy, WithPubService, WithPrivateService("change-prs"),
					WithDeployRef("blah")),
				deploy("pod", "blah"),
				svcpub("pod", "change"),
				svcpriv("pod", "change", svcWithName("change-prs")),
				endpointspub("pod", "change", WithSubsets),
				endpointspriv("pod", "change", withOtherSubsets, epsWithName("change-prs")),
				activatorEndpoints(WithSubsets),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{
				Object: endpointspub("pod", "change", withOtherSubsets),
			}},
		}, {
			Name: "proxy mode; pod change - activator",
			Key:  "pod/change",
			Objects: []runtime.Object{
				SKS("pod", "change", markNoEndpoints, WithPubService, withHTTP2Protocol,
					WithPrivateService("change-taylor"), WithProxyMode, WithDeployRef("blah")),
				deploy("pod", "blah"),
				svcpub("pod", "change", withHTTP2),
				svcpriv("pod", "change", withHTTP2Priv, svcWithName("change-taylor")),
				endpointspub("pod", "change", WithSubsets),
				endpointspriv("pod", "change", epsWithName("change-taylor")),
				activatorEndpoints(withOtherSubsets),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{
				Object: endpointspub("pod", "change", withOtherSubsets),
			}},
		}, {
			Name: "serving mode; serving pod comes online",
			Key:  "pod/change",
			Objects: []runtime.Object{
				SKS("pod", "change", markNoEndpoints, WithPubService,
					WithPrivateService("change-gretsch"), WithDeployRef("blah")),
				deploy("pod", "blah"),
				svcpub("pod", "change"),
				svcpriv("pod", "change", svcWithName("change-gretsch")),
				endpointspub("pod", "change", withOtherSubsets),
				endpointspriv("pod", "change", WithSubsets, epsWithName("change-gretsch")),
				activatorEndpoints(withOtherSubsets),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: SKS("pod", "change",
					WithDeployRef("blah"), markHappy, WithPubService, WithPrivateService("change-gretsch")),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "pod/change"`),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{
				Object: endpointspub("pod", "change", WithSubsets),
			}},
		}, {
			Name: "serving mode; no backend endpoints",
			Key:  "pod/change",
			Objects: []runtime.Object{
				SKS("pod", "change", WithSKSReady, WithPubService, withHTTP2Protocol,
					WithPrivateService("change-rickenbecker"), WithDeployRef("blah")),
				deploy("pod", "blah"),
				svcpub("pod", "change", withHTTP2),
				svcpriv("pod", "change", withHTTP2Priv, svcWithName("change-rickenbecker")),
				endpointspub("pod", "change", WithSubsets),                         // We had endpoints...
				endpointspriv("pod", "change", epsWithName("change-rickenbecker")), // but now we don't.
				activatorEndpoints(withOtherSubsets),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: SKS("pod", "change", withHTTP2Protocol,
					WithDeployRef("blah"), markNoEndpoints, WithPubService, WithPrivateService("change-rickenbecker")),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "pod/change"`),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{
				Object: endpointspub("pod", "change", withOtherSubsets),
			}},
		}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &reconciler{
			Base:              rpkg.NewBase(ctx, controllerAgentName, cmw),
			sksLister:         listers.GetServerlessServiceLister(),
			serviceLister:     listers.GetK8sServiceLister(),
			endpointsLister:   listers.GetEndpointsLister(),
			psInformerFactory: presources.NewPodScalableInformerFactory(ctx),
		}
	}))
}

// withOtherSubsets uses different IP set than functional::withSubsets.
func withOtherSubsets(ep *corev1.Endpoints) {
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{IP: "127.0.0.2"}},
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

func svcWithName(n string) K8sServiceOption {
	return func(s *corev1.Service) {
		s.GenerateName = ""
		s.Name = n
	}
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
			Name:      activator.K8sServiceName,
		},
	}
	for _, opt := range eo {
		opt(ep)
	}
	return ep
}

func epsWithName(n string) EndpointsOption {
	return func(e *corev1.Endpoints) {
		e.Name = n
	}
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
