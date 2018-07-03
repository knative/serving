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
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

var one int32 = 1

func TestMakeAutoscalerService(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		want *corev1.Service
	}{{
		name: "name is bar",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pkg.GetServingSystemNamespace(),
				Name:      "bar-autoscaler",
				Labels: map[string]string{
					serving.AutoscalerLabelKey: "bar-autoscaler",
					serving.RevisionLabelKey:   "bar",
					serving.RevisionUID:        "1234",
					appLabelKey:                "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: corev1.ServiceSpec{
				Ports: autoscalerServicePorts,
				Type:  "NodePort",
				Selector: map[string]string{
					serving.AutoscalerLabelKey: "bar-autoscaler",
				},
			},
		},
	}, {
		name: "name is baz",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "baz",
				UID:       "1234",
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pkg.GetServingSystemNamespace(),
				Name:      "baz-autoscaler",
				Labels: map[string]string{
					serving.AutoscalerLabelKey: "baz-autoscaler",
					serving.RevisionLabelKey:   "baz",
					serving.RevisionUID:        "1234",
					appLabelKey:                "baz",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "baz",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: corev1.ServiceSpec{
				Ports: autoscalerServicePorts,
				Type:  "NodePort",
				Selector: map[string]string{
					serving.AutoscalerLabelKey: "baz-autoscaler",
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeAutoscalerService(test.rev)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeAutoscalerService (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMakeAutoscalerDeployment(t *testing.T) {
	tests := []struct {
		name     string
		rev      *v1alpha1.Revision
		image    string
		replicas int32
		want     *appsv1.Deployment
	}{{
		name: "no owner labels or annotations, concurrency=single",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ConcurrencyModel: "Single",
			},
		},
		image:    "autoscaler:latest",
		replicas: 1,
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pkg.GetServingSystemNamespace(),
				Name:      "bar-autoscaler",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					appLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						appLabelKey:              "bar",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.AutoscalerLabelKey: "bar-autoscaler",
							serving.RevisionLabelKey:   "bar",
							serving.RevisionUID:        "1234",
							appLabelKey:                "bar",
						},
						Annotations: map[string]string{
							SidecarIstioInjectAnnotation: "true",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:      "autoscaler",
							Image:     "autoscaler:latest",
							Resources: autoscalerResources,
							Ports:     autoscalerPorts,
							Env: []corev1.EnvVar{{
								Name:  "SERVING_NAMESPACE",
								Value: "foo",
							}, {
								Name:  "SERVING_DEPLOYMENT",
								Value: "bar-deployment",
							}, {
								Name: "SERVING_CONFIGURATION",
								// No owner reference
							}, {
								Name:  "SERVING_REVISION",
								Value: "bar",
							}, {
								Name:  "SERVING_AUTOSCALER_PORT",
								Value: strconv.Itoa(AutoscalerPort),
							}},
							Args:         []string{"-concurrencyModel=Single"},
							VolumeMounts: autoscalerVolumeMounts,
						}},
						ServiceAccountName: "autoscaler",
						Volumes:            autoscalerVolumes,
					},
				},
			},
		},
	}, {
		name: "no owner labels or annotations, concurrency=multi",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ConcurrencyModel: "Multi",
			},
		},
		image:    "autoscaler:latest",
		replicas: 1,
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pkg.GetServingSystemNamespace(),
				Name:      "bar-autoscaler",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					appLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						appLabelKey:              "bar",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.AutoscalerLabelKey: "bar-autoscaler",
							serving.RevisionLabelKey:   "bar",
							serving.RevisionUID:        "1234",
							appLabelKey:                "bar",
						},
						Annotations: map[string]string{
							SidecarIstioInjectAnnotation: "true",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:      "autoscaler",
							Image:     "autoscaler:latest",
							Resources: autoscalerResources,
							Ports:     autoscalerPorts,
							Env: []corev1.EnvVar{{
								Name:  "SERVING_NAMESPACE",
								Value: "foo",
							}, {
								Name:  "SERVING_DEPLOYMENT",
								Value: "bar-deployment",
							}, {
								Name: "SERVING_CONFIGURATION",
								// No owner reference
							}, {
								Name:  "SERVING_REVISION",
								Value: "bar",
							}, {
								Name:  "SERVING_AUTOSCALER_PORT",
								Value: strconv.Itoa(AutoscalerPort),
							}},
							Args:         []string{"-concurrencyModel=Multi"},
							VolumeMounts: autoscalerVolumeMounts,
						}},
						ServiceAccountName: "autoscaler",
						Volumes:            autoscalerVolumes,
					},
				},
			},
		},
	}, {
		name: "owner config as env var",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "my-parent-config",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: v1alpha1.RevisionSpec{
				ConcurrencyModel: "Multi",
			},
		},
		image:    "autoscaler:latest",
		replicas: 1,
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pkg.GetServingSystemNamespace(),
				Name:      "bar-autoscaler",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					appLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						appLabelKey:              "bar",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.AutoscalerLabelKey: "bar-autoscaler",
							serving.RevisionLabelKey:   "bar",
							serving.RevisionUID:        "1234",
							appLabelKey:                "bar",
						},
						Annotations: map[string]string{
							SidecarIstioInjectAnnotation: "true",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:      "autoscaler",
							Image:     "autoscaler:latest",
							Resources: autoscalerResources,
							Ports:     autoscalerPorts,
							Env: []corev1.EnvVar{{
								Name:  "SERVING_NAMESPACE",
								Value: "foo",
							}, {
								Name:  "SERVING_DEPLOYMENT",
								Value: "bar-deployment",
							}, {
								Name:  "SERVING_CONFIGURATION",
								Value: "my-parent-config",
							}, {
								Name:  "SERVING_REVISION",
								Value: "bar",
							}, {
								Name:  "SERVING_AUTOSCALER_PORT",
								Value: strconv.Itoa(AutoscalerPort),
							}},
							Args:         []string{"-concurrencyModel=Multi"},
							VolumeMounts: autoscalerVolumeMounts,
						}},
						ServiceAccountName: "autoscaler",
						Volumes:            autoscalerVolumes,
					},
				},
			},
		},
	}, {
		name: "propagate labels",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					"ooga": "booga",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				ConcurrencyModel: "Multi",
			},
		},
		image:    "autoscaler:latest",
		replicas: 1,
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pkg.GetServingSystemNamespace(),
				Name:      "bar-autoscaler",
				Labels: map[string]string{
					"ooga": "booga",
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					appLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						"ooga":                   "booga",
						appLabelKey:              "bar",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.AutoscalerLabelKey: "bar-autoscaler",
							serving.RevisionLabelKey:   "bar",
							"ooga":              "booga",
							serving.RevisionUID: "1234",
							appLabelKey:         "bar",
						},
						Annotations: map[string]string{
							SidecarIstioInjectAnnotation: "true",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:      "autoscaler",
							Image:     "autoscaler:latest",
							Resources: autoscalerResources,
							Ports:     autoscalerPorts,
							Env: []corev1.EnvVar{{
								Name:  "SERVING_NAMESPACE",
								Value: "foo",
							}, {
								Name:  "SERVING_DEPLOYMENT",
								Value: "bar-deployment",
							}, {
								Name: "SERVING_CONFIGURATION",
								// No owner reference
							}, {
								Name:  "SERVING_REVISION",
								Value: "bar",
							}, {
								Name:  "SERVING_AUTOSCALER_PORT",
								Value: strconv.Itoa(AutoscalerPort),
							}},
							Args:         []string{"-concurrencyModel=Multi"},
							VolumeMounts: autoscalerVolumeMounts,
						}},
						ServiceAccountName: "autoscaler",
						Volumes:            autoscalerVolumes,
					},
				},
			},
		},
	}, {
		name: "propagate annotations",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Annotations: map[string]string{
					"ooga": "booga",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				ConcurrencyModel: "Multi",
			},
		},
		image:    "autoscaler:latest",
		replicas: 1,
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pkg.GetServingSystemNamespace(),
				Name:      "bar-autoscaler",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					appLabelKey:              "bar",
				},
				Annotations: map[string]string{
					"ooga": "booga",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						appLabelKey:              "bar",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.AutoscalerLabelKey: "bar-autoscaler",
							serving.RevisionLabelKey:   "bar",
							serving.RevisionUID:        "1234",
							appLabelKey:                "bar",
						},
						Annotations: map[string]string{
							"ooga": "booga",
							SidecarIstioInjectAnnotation: "true",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:      "autoscaler",
							Image:     "autoscaler:latest",
							Resources: autoscalerResources,
							Ports:     autoscalerPorts,
							Env: []corev1.EnvVar{{
								Name:  "SERVING_NAMESPACE",
								Value: "foo",
							}, {
								Name:  "SERVING_DEPLOYMENT",
								Value: "bar-deployment",
							}, {
								Name: "SERVING_CONFIGURATION",
								// No owner reference
							}, {
								Name:  "SERVING_REVISION",
								Value: "bar",
							}, {
								Name:  "SERVING_AUTOSCALER_PORT",
								Value: strconv.Itoa(AutoscalerPort),
							}},
							Args:         []string{"-concurrencyModel=Multi"},
							VolumeMounts: autoscalerVolumeMounts,
						}},
						ServiceAccountName: "autoscaler",
						Volumes:            autoscalerVolumes,
					},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeAutoscalerDeployment(test.rev, test.image, test.replicas)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("MakeAutoscalerDeployment (-want, +got) = %v", diff)
			}
		})
	}
}
