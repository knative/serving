/*
Copyright 2018 The Knative Authors.

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

package testing

import (
	"testing"

	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/controller"
	. "github.com/knative/pkg/logging/testing"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/reconciler"
)

type Ctor func(*Listers, reconciler.Options) controller.Reconciler

func MakeFactory(ctor Ctor) Factory {
	return func(t *testing.T, r *TableRow) (controller.Reconciler, ExtractActions) {
		ls := NewListers(r.Objects)

		kubeClient := fakekubeclientset.NewSimpleClientset(ls.GetKubeObjects()...)
		sharedClient := fakesharedclientset.NewSimpleClientset(ls.GetSharedObjects()...)
		client := fakeclientset.NewSimpleClientset(ls.GetServingObjects()...)
		buildClient := fakebuildclientset.NewSimpleClientset(ls.GetBuildObjects()...)

		// Set up our Controller from the fakes.
		c := ctor(&ls, reconciler.Options{
			KubeClientSet:    kubeClient,
			SharedClientSet:  sharedClient,
			BuildClientSet:   buildClient,
			ServingClientSet: client,
			Logger:           TestLogger(t),
		})

		for _, reactor := range r.WithReactors {
			kubeClient.PrependReactor("*", "*", reactor)
			sharedClient.PrependReactor("*", "*", reactor)
			client.PrependReactor("*", "*", reactor)
			buildClient.PrependReactor("*", "*", reactor)
		}

		// Validate all Create operations through the serving client.
		client.PrependReactor("create", "*", ValidateCreates)
		client.PrependReactor("update", "*", ValidateUpdates)

		return c, func() ([]clientgotesting.CreateAction, []clientgotesting.UpdateAction,
			[]clientgotesting.DeleteAction, []clientgotesting.PatchAction) {
			return extractActions(t, sharedClient, buildClient, client, kubeClient)
		}
	}
}

type hasActions interface {
	Actions() []clientgotesting.Action
}

func extractActions(t *testing.T, clients ...hasActions) (
	createActions []clientgotesting.CreateAction,
	updateActions []clientgotesting.UpdateAction,
	deleteActions []clientgotesting.DeleteAction,
	patchActions []clientgotesting.PatchAction,
) {

	for _, c := range clients {
		for _, action := range c.Actions() {
			switch action.GetVerb() {
			case "create":
				createActions = append(createActions,
					action.(clientgotesting.CreateAction))
			case "update":
				updateActions = append(updateActions,
					action.(clientgotesting.UpdateAction))
			case "delete":
				deleteActions = append(deleteActions,
					action.(clientgotesting.DeleteAction))
			case "patch":
				patchActions = append(patchActions,
					action.(clientgotesting.PatchAction))
			default:
				t.Errorf("Unexpected verb %v: %+v", action.GetVerb(), action)
			}
		}
	}
	return
}

func BuildRow(f func() TableRow) TableRow {
	return f()
}
