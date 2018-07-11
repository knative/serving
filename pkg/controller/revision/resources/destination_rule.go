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
        "github.com/knative/serving/pkg"
        "github.com/knative/serving/pkg/apis/istio/v1alpha3"
        "github.com/knative/serving/pkg/apis/serving/v1alpha1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "github.com/knative/serving/pkg/controller"
        "github.com/knative/serving/pkg/controller/revision/resources/names"
)

func MakeRevisionDestinationRule(rev *v1alpha1.Revision) *v1alpha3.DestinationRule {
        return &v1alpha3.DestinationRule{
                ObjectMeta: metav1.ObjectMeta{
                        Name:            names.DestinationRule(rev),
                        Namespace:       rev.Namespace,
                        Labels:          map[string]string{"revision": rev.Name},
                        OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
                }, 
                Spec: v1alpha3.DestinationRuleSpec{
                        Host:  names.K8sServiceFullName(rev),
                        TrafficPolicy: &v1alpha3.TrafficPolicy{
                                Tls: &v1alpha3.TLSSettings{
                                        Mode: v1alpha3.TLSmodeIstioMutual,
                                },
                        },
                },
        }
}

func MakeAutoscalerDestinationRule(rev *v1alpha1.Revision) *v1alpha3.DestinationRule {
        return &v1alpha3.DestinationRule{
                ObjectMeta: metav1.ObjectMeta{
                        Name:            names.AutoscalerDestinationRule(rev),
                        Namespace:       pkg.GetServingSystemNamespace(),
                        Labels:          map[string]string{"revision": rev.Name},
                        OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
                }, 
                Spec: v1alpha3.DestinationRuleSpec{
                        Host:  names.AutoscalerFullName(rev),
                        TrafficPolicy: &v1alpha3.TrafficPolicy{
                                Tls: &v1alpha3.TLSSettings{
                                        Mode: v1alpha3.TLSmodeIstioMutual,
                                },
                        },
                },
        }
}
