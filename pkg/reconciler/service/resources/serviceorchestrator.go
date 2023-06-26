/*
Copyright 2023 The Knative Authors

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
	"fmt"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"

	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/service/resources/names"
)

var (
	OverSubRatio = 20
)

type RevisionRecord struct {
	MinScale *int32
	MaxScale *int32
	Name     string
	Replicas *int32
}

// MakeServiceOrchestrator creates a ServiceOrchestrator from a Service object.
func MakeServiceOrchestrator(service *v1.Service, config *v1.Configuration, route *v1.Route, records map[string]RevisionRecord,
	logging *zap.SugaredLogger) *v1.ServiceOrchestrator {
	// The ultimate revision target comes from the service.
	logging.Infof("MakeServiceOrchestrator MakeServiceOrchestrator MakeServiceOrchestrator MakeServiceOrchestrator MakeServiceOrchestrators")

	logging.Infof("check the service R")
	logging.Info(service)
	logging.Infof("check the service status")
	logging.Info(service.Status)
	var initialRevisionStatus, ultimateRevisionTarget []v1.RevisionTarget

	lastRN := kmeta.ChildName(config.Name, fmt.Sprintf("-%05d", config.Generation))

	logging.Info("lastRN is")
	logging.Info(lastRN)

	logging.Info(len(records))
	logging.Info(records)

	if service.Spec.Traffic == nil || len(service.Spec.Traffic) == 0 {
		ultimateRevisionTarget = make([]v1.RevisionTarget, 1, 1)
		target := v1.RevisionTarget{}
		target.LatestRevision = ptr.Bool(true)
		target.RevisionName = lastRN
		target.Percent = ptr.Int64(90)
		target.MinScale = nil
		target.MaxScale = nil
		if val, ok := records[target.RevisionName]; ok {
			if val.MinScale != nil {
				target.MinScale = ptr.Int32(*val.MinScale)
			}
			if val.MaxScale != nil {
				target.MaxScale = ptr.Int32(*val.MaxScale)
			}
		} else {
			// Get min and max scales from the service
			if val, ok := service.Annotations[autoscaling.MinScaleAnnotationKey]; ok {
				i, err := strconv.ParseInt(val, 10, 32)
				if err == nil {
					target.MinScale = ptr.Int32(int32(i))
				}
			}

			if val, ok := service.Annotations[autoscaling.MaxScaleAnnotationKey]; ok {
				i, err := strconv.ParseInt(val, 10, 32)
				if err == nil {
					target.MaxScale = ptr.Int32(int32(i))
				}
			}
		}
		ultimateRevisionTarget[0] = target
	} else {
		logging.Infof("run this part to create for the first version run this part to create for the first version run this part to create for the first version run this part to create for the first version")

		ultimateRevisionTarget = make([]v1.RevisionTarget, len(service.Spec.Traffic), len(service.Spec.Traffic))
		target := v1.RevisionTarget{}
		for i, traffic := range service.Spec.Traffic {
			if traffic.RevisionName == lastRN || *traffic.LatestRevision {
				logging.Infof("run this part to create for the first version run this part to create for the first version run this part to create for the first version run this part to create for the first version")
				logging.Info(lastRN)

				target.LatestRevision = ptr.Bool(true)
				target.RevisionName = lastRN
			} else {
				target.LatestRevision = ptr.Bool(false)
				target.RevisionName = traffic.RevisionName
			}
			target.Percent = ptr.Int64(*traffic.Percent)
			target.MinScale = nil
			target.MaxScale = nil
			if val, ok := records[target.RevisionName]; ok {
				logging.Info("found revision")
				if val.MinScale != nil {
					logging.Info("found set min")
					target.MinScale = ptr.Int32(*val.MinScale)
				}
				if val.MaxScale != nil {
					logging.Info("found set max")
					target.MaxScale = ptr.Int32(*val.MaxScale)
				}
			} else {
				// Get min and max scales from the service
				if val, ok := service.Annotations[autoscaling.MinScaleAnnotationKey]; ok {
					i, err := strconv.ParseInt(val, 10, 32)
					if err == nil {
						target.MinScale = ptr.Int32(int32(i))
					}

				}

				if val, ok := service.Annotations[autoscaling.MaxScaleAnnotationKey]; ok {
					i, err := strconv.ParseInt(val, 10, 32)
					if err == nil {
						target.MaxScale = ptr.Int32(int32(i))
					}
				}
			}
			ultimateRevisionTarget[i] = target
		}

	}

	if route == nil || route.Status.Traffic == nil || len(route.Status.Traffic) == 0 {

		initialRevisionStatus = nil
		initialRevisionStatus = ultimateRevisionTarget
	} else {
		logging.Infof("run this part to create for the first version run this part to create for the first version run this part to create for the first version run this part to create for the first version")

		initialRevisionStatus = make([]v1.RevisionTarget, len(route.Status.Traffic), len(route.Status.Traffic))
		target := v1.RevisionTarget{}
		for i, traffic := range route.Status.Traffic {
			if traffic.RevisionName == lastRN || *traffic.LatestRevision {
				target.LatestRevision = ptr.Bool(true)
				target.RevisionName = lastRN
			} else {
				target.LatestRevision = ptr.Bool(false)
				target.RevisionName = traffic.RevisionName
			}
			target.Percent = ptr.Int64(*traffic.Percent)
			target.MinScale = nil
			target.MaxScale = nil
			if val, ok := records[target.RevisionName]; ok {
				if val.MinScale != nil {
					target.MinScale = ptr.Int32(*val.MinScale)
				}
				if val.MaxScale != nil {
					target.MaxScale = ptr.Int32(*val.MaxScale)
				}
			}
			initialRevisionStatus[i] = target
		}
	}

	// The initial revision status comes from the route. We set the first stage revision status to the
	// initial revision status as well.

	so := &v1.ServiceOrchestrator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.ServiceOrchestrator(service),
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(service),
			},
		},
		Spec: v1.ServiceOrchestratorSpec{
			RevisionTarget:        ultimateRevisionTarget,
			InitialRevisionStatus: initialRevisionStatus,
		},
	}

	so.Status.StageRevisionStatus = append([]v1.RevisionTarget{}, initialRevisionStatus...)
	return so
}
