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
        "github.com/knative/serving/pkg/controller/revision/resources/names"
        authv1alpha1 "github.com/knative/serving/pkg/apis/istio/authentication/v1alpha1"
        "github.com/knative/serving/pkg/apis/serving/v1alpha1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "github.com/knative/serving/pkg/controller"
)

func MakeAuthenticationPolicy(rev *v1alpha1.Revision) *authv1alpha1.Policy {
        return &authv1alpha1.Policy{
                ObjectMeta: metav1.ObjectMeta{
                        Name:            names.AuthenticationPolicy(rev),
                        Namespace:       rev.Namespace,
                        Labels:          map[string]string{"revision": rev.Name},
                        OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
                }, 
                Spec: authv1alpha1.PolicySpec{
                        Targets: []authv1alpha1.TargetSelector{
                                authv1alpha1.TargetSelector{
                                        // this should be full name?
                                        Name: names.K8sService(rev),
                                },
                        },
                        Peers: []authv1alpha1.PeerAuthenticationMethod{
                                authv1alpha1.PeerAuthenticationMethod{
                                        Mtls: &authv1alpha1.MutualTls{
                                                Mode: authv1alpha1.ModeStrict,
                                        },
                                },
                        },
                },
        }
}
