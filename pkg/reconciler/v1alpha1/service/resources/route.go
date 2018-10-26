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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
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
			Labels: makeLabels(service),
		},
	}

	if service.Spec.Release != nil {
		rolloutPercent := service.Spec.Release.RolloutPercent
		numRevisions := len(service.Spec.Release.Revisions)

		// Configure the 'current' route
		currentRevisionName := service.Spec.Release.Revisions[0]
		ttCurrent := v1alpha1.TrafficTarget{
			Name:         "current",
			Percent:      100 - rolloutPercent,
			RevisionName: currentRevisionName,
		}
		c.Spec.Traffic = append(c.Spec.Traffic, ttCurrent)

		// Configure the 'candidate' route
		if numRevisions == 2 {
			candidateRevisionName := service.Spec.Release.Revisions[1]
			ttCandidate := v1alpha1.TrafficTarget{
				Name:         "candidate",
				Percent:      rolloutPercent,
				RevisionName: candidateRevisionName,
			}
			c.Spec.Traffic = append(c.Spec.Traffic, ttCandidate)
		}

		// Configure the 'latest' route
		ttLatest := v1alpha1.TrafficTarget{
			Name:              "latest",
			ConfigurationName: names.Configuration(service),
			Percent:           0,
		}
		c.Spec.Traffic = append(c.Spec.Traffic, ttLatest)
	} else if service.Spec.RunLatest != nil {
		tt := v1alpha1.TrafficTarget{
			ConfigurationName: names.Configuration(service),
			Percent:           100,
		}
		c.Spec.Traffic = append(c.Spec.Traffic, tt)
	} else if service.Spec.Pinned != nil {
		tt := v1alpha1.TrafficTarget{
			RevisionName: service.Spec.Pinned.RevisionName,
			Percent:      100,
		}
		c.Spec.Traffic = append(c.Spec.Traffic, tt)
	} else {
		// manual does not have a route and should not reach this path
		return nil, errors.New("malformed Service: MakeRoute requires one of runLatest, pinned, or release must be present")
	}

	return c, nil
}
