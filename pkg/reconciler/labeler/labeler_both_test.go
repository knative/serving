/*
Copyright 2020 The Knative Authors.

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

package labeler

import (
	"context"
	"testing"

	// Inject the fake informers that this controller needs.
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgotesting "k8s.io/client-go/testing"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	cfgmap "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	. "knative.dev/pkg/reconciler/testing"
	labelerv1 "knative.dev/serving/pkg/reconciler/labeler/v1"
	labelerv2 "knative.dev/serving/pkg/reconciler/labeler/v2"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

func TestV2ReconcileAllowed(t *testing.T) {
	now := metav1.Now()
	fakeTime := now.Time

	table := TableTest{{
		Name: "change configurations",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Allowed),
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-change", "new-config", WithRouteFinalizer),

			rev("default", "old-config",
				WithRevisionLabel("serving.knative.dev/route", "config-change"),
				WithRevisionAnn("serving.knative.dev/routes", "config-change"),
				WithRoutingState(v1.RoutingStateActive)),

			simpleConfig("default", "new-config"),
			rev("default", "new-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			// v1 sync
			patchRemoveLabel("default", rev("default", "old-config").Name,
				"serving.knative.dev/route"),
			patchAddLabel("default", rev("default", "new-config").Name, "serving.knative.dev/route", "config-change"),
			patchAddLabel("default", "new-config", "serving.knative.dev/route", "config-change"),

			// v2 sync
			patchRemoveRouteAnn("default", rev("default", "old-config").Name),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "new-config").Name, "config-change", now.Time),
			patchAddRouteAnn("default", "new-config", "config-change"),
		},
		Key: "default/config-change",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		clock := clock.NewFakeClock(fakeTime)
		client := servingclient.Get(ctx)
		cLister := listers.GetConfigurationLister()
		cIndexer := listers.IndexerFor(&v1.Configuration{})
		rLister := listers.GetRevisionLister()
		rIndexer := listers.IndexerFor(&v1.Revision{})
		r := &Reconciler{
			caccV1: labelerv1.NewConfigurationAccessor(client, &NullTracker{}, cLister),
			caccV2: labelerv2.NewConfigurationAccessor(client, &NullTracker{}, cLister, cIndexer, clock),
			raccV1: labelerv1.NewRevisionAccessor(client, &NullTracker{}, rLister),
			raccV2: labelerv2.NewRevisionAccessor(client, &NullTracker{}, rLister, rIndexer, clock),
		}

		return routereconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetRouteLister(), controller.GetEventRecorder(ctx), r)
	}))
}
