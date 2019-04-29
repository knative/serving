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

package resources

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/reconciler/service/resources/names"
	"github.com/knative/serving/pkg/resources"
)

// MakeRoute creates a Route from a Service object.
func MakeRoute(service *v1alpha1.Service) (*v1alpha1.Route, error) {
	c := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Route(service),
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(service),
			},
			Annotations: service.GetAnnotations(),
			Labels: resources.UnionMaps(service.GetLabels(), map[string]string{
				// Add this service's name to the route annotations.
				serving.ServiceLabelKey: service.Name,
			}),
		},
	}

	if service.Spec.DeprecatedRelease != nil {
		rolloutPercent := service.Spec.DeprecatedRelease.RolloutPercent
		numRevisions := len(service.Spec.DeprecatedRelease.Revisions)

		// Configure the 'current' route.
		ttCurrent := v1alpha1.TrafficTarget{
			TrafficTarget: v1beta1.TrafficTarget{
				Tag:     v1alpha1.CurrentTrafficTarget,
				Percent: 100 - rolloutPercent,
			},
		}
		currentRevisionName := service.Spec.DeprecatedRelease.Revisions[0]

		// If the `current` revision refers to the well known name of the last
		// known revision, use `Configuration` instead.
		// Same for the `candidate` below.
		// Part of #2819.
		if currentRevisionName == v1alpha1.ReleaseLatestRevisionKeyword {
			ttCurrent.ConfigurationName = names.Configuration(service)
		} else {
			ttCurrent.RevisionName = currentRevisionName
		}
		c.Spec.Traffic = append(c.Spec.Traffic, ttCurrent)

		// Configure the 'candidate' route.
		if numRevisions == 2 {
			ttCandidate := v1alpha1.TrafficTarget{
				TrafficTarget: v1beta1.TrafficTarget{
					Tag:     v1alpha1.CandidateTrafficTarget,
					Percent: rolloutPercent,
				},
			}
			candidateRevisionName := service.Spec.DeprecatedRelease.Revisions[1]
			if candidateRevisionName == v1alpha1.ReleaseLatestRevisionKeyword {
				ttCandidate.ConfigurationName = names.Configuration(service)
			} else {
				ttCandidate.RevisionName = candidateRevisionName
			}
			c.Spec.Traffic = append(c.Spec.Traffic, ttCandidate)
		}

		// Configure the 'latest' route.
		ttLatest := v1alpha1.TrafficTarget{
			TrafficTarget: v1beta1.TrafficTarget{
				Tag:               v1alpha1.LatestTrafficTarget,
				ConfigurationName: names.Configuration(service),
				Percent:           0,
			},
		}
		c.Spec.Traffic = append(c.Spec.Traffic, ttLatest)
	} else if service.Spec.DeprecatedRunLatest != nil {
		tt := v1alpha1.TrafficTarget{
			TrafficTarget: v1beta1.TrafficTarget{
				ConfigurationName: names.Configuration(service),
				Percent:           100,
			},
		}
		c.Spec.Traffic = append(c.Spec.Traffic, tt)
	} else if service.Spec.DeprecatedPinned != nil {
		tt := v1alpha1.TrafficTarget{
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName: service.Spec.DeprecatedPinned.RevisionName,
				Percent:      100,
			},
		}
		c.Spec.Traffic = append(c.Spec.Traffic, tt)
	} else if service.Spec.DeprecatedManual != nil {
		// DeprecatedManual does not have a route and should not reach this path.
		return nil, errors.New("malformed Service: MakeRoute requires one of runLatest, pinned, or release must be present")
	} else {
		c.Spec = *service.Spec.RouteSpec.DeepCopy()
		// Fill in any missing ConfigurationName fields when translating
		// from Service to Route.
		for idx := range c.Spec.Traffic {
			if c.Spec.Traffic[idx].RevisionName == "" {
				c.Spec.Traffic[idx].ConfigurationName = names.Configuration(service)
			}
		}
	}

	return c, nil
}
