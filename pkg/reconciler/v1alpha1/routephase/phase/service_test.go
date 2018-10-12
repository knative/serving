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

package phase

import (
	"testing"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

func TestServiceReconcile(t *testing.T) {
	scenarios := PhaseTests{
		{
			Name:     "first-reconcile",
			Resource: simpleRunLatest("default", "first-reconcile", "not-ready"),
			ExpectedStatus: v1alpha1.RouteStatus{
				DomainInternal: "first-reconcile.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "first-reconcile.default.svc.cluster.local"},
			},
			ExpectedCreates: Creates{
				resources.MakeK8sService(
					simpleRunLatest("default", "first-reconcile", "not-ready"),
				),
			},
		}, {
			Name:     "create-service-fails",
			Resource: simpleRunLatest("default", "first-reconcile", "not-ready"),
			Failures: Failures{
				InduceFailure("create", "services"),
			},
			ExpectError:    true,
			ExpectedStatus: v1alpha1.RouteStatus{},
			ExpectedCreates: Creates{
				resources.MakeK8sService(
					simpleRunLatest("default", "first-reconcile", "not-ready"),
				),
			},
		}, {
			Name:     "steady-state",
			Resource: simpleRunLatest("default", "steady-state", "not-ready"),
			Objects: Objects{
				resources.MakeK8sService(
					simpleRunLatest("default", "steady-state", "not-ready"),
				),
			},
			ExpectedStatus: v1alpha1.RouteStatus{
				DomainInternal: "steady-state.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "steady-state.default.svc.cluster.local"},
			},
		}, {
			Name:     "service-spec-change",
			Resource: simpleRunLatest("default", "service-change", "not-ready"),
			Objects: Objects{
				mutateService(
					resources.MakeK8sService(
						simpleRunLatest("default", "service-change", "not-ready"),
					),
				),
			},
			ExpectedUpdates: Updates{
				resources.MakeK8sService(
					simpleRunLatest("default", "service-change", "not-ready"),
				),
			},
			ExpectedStatus: v1alpha1.RouteStatus{
				DomainInternal: "service-change.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "service-change.default.svc.cluster.local"},
			},
		}, {
			Name:     "service-update-failed",
			Resource: simpleRunLatest("default", "service-change", "not-ready"),
			Objects: Objects{
				mutateService(
					resources.MakeK8sService(
						simpleRunLatest("default", "service-change", "not-ready"),
					),
				),
			},
			Failures: Failures{
				InduceFailure("update", "services"),
			},
			ExpectError:    true,
			ExpectedStatus: v1alpha1.RouteStatus{},
			ExpectedUpdates: Updates{
				resources.MakeK8sService(
					simpleRunLatest("default", "service-change", "not-ready"),
				),
			},
		}, {
			Name:     "allow cluster ip",
			Resource: simpleRunLatest("default", "cluster-ip", "config"),
			Objects: Objects{
				setClusterIP(
					resources.MakeK8sService(
						simpleRunLatest("default", "cluster-ip", "config"),
					),
					"127.0.0.1",
				),
			},
			ExpectedStatus: v1alpha1.RouteStatus{
				DomainInternal: "cluster-ip.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "cluster-ip.default.svc.cluster.local"},
			},
		},
	}

	scenarios.Run(t, PhaseSetup(NewK8sService))
}

func mutateService(svc *corev1.Service) *corev1.Service {
	// Thor's Hammer
	svc.Spec = corev1.ServiceSpec{}
	return svc
}

func simpleRunLatest(namespace, name, config string) *v1alpha1.Route {
	return routeWithTraffic(namespace, name, v1alpha1.TrafficTarget{
		ConfigurationName: config,
		Percent:           100,
	})
}

func routeWithTraffic(namespace, name string, traffic ...v1alpha1.TrafficTarget) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: traffic,
		},
	}
}

func setClusterIP(svc *corev1.Service, ip string) *corev1.Service {
	svc.Spec.ClusterIP = ip
	return svc
}
