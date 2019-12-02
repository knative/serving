/*
Copyright 2019 The Knative Authors.

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

package revision

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sinformers "k8s.io/client-go/informers"
	corev1informer "k8s.io/client-go/informers/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

var (
	containerName = "my-container-name"
)

type containerOption func(*corev1.Container)
type revisionOption func(*v1alpha1.Revision)

func container(container *corev1.Container, opts ...containerOption) corev1.Container {
	for _, option := range opts {
		option(container)
	}
	return *container
}

func revision(opts ...revisionOption) *v1alpha1.Revision {
	defaultRevision := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
			UID:       "1234",
			Labels: map[string]string{
				serving.ConfigurationLabelKey: "cfg",
				serving.ServiceLabelKey:       "svc",
				serving.RouteLabelKey:         "im-a-route",
			},
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1.RevisionSpec{
				TimeoutSeconds: ptr.Int64(45),
				PodSpec: corev1.PodSpec{
					InitContainers: nil,
					Containers: []corev1.Container{{
						Name:  "containerName",
						Image: "busybox",
					}},
				},
			},
		},
	}
	for _, option := range opts {
		option(defaultRevision)
	}
	return defaultRevision
}

func TestVerifySecrets(t *testing.T) {
	tests := []struct {
		name        string
		rev         *v1alpha1.Revision
		si          corev1informer.SecretInformer
		expectedErr string
	}{{
		name: "secrets (from volume source) found",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.GetContainer().VolumeMounts = []corev1.VolumeMount{{
					Name:      "asdf1",
					MountPath: "/asdf1",
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.Spec.Volumes = []corev1.Volume{{
					Name: "asdf1",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "asdf1",
						},
					},
				}}
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: addSecretToInformer(kubefake.NewSimpleClientset(), "asdf1", "foo")(getSecretInformer()),
	}, {
		name: "secrets (from volume source) not found",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.GetContainer().Ports = []corev1.ContainerPort{{
					ContainerPort: 8888,
				}}
				revision.Spec.GetContainer().VolumeMounts = []corev1.VolumeMount{{
					Name:      "asdf1",
					MountPath: "/asdf1",
				}, {
					Name:      "asdf2",
					MountPath: "/asdf2",
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				// Adding two secrets that do not exist. We should see both of them in the error
				revision.Spec.Volumes = []corev1.Volume{{
					Name: "asdf1",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "asdf1",
						},
					},
				}, {
					Name: "asdf2",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "asdf2",
						},
					},
				}}
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: getSecretInformer(),
		expectedErr: `secret "asdf1" not found: spec.volumes[0].volumeSource.secretName
secret "asdf2" not found: spec.volumes[1].volumeSource.secretName`,
	}, {
		name: "secrets (from volume source) not found but optional is true",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.GetContainer().VolumeMounts = []corev1.VolumeMount{{
					Name:      "asdf",
					MountPath: "/asdf",
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.Spec.Volumes = []corev1.Volume{{
					Name: "asdf",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "asdf",
							Optional:   ptr.Bool(true),
						},
					},
				}}
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: getSecretInformer(),
	}, {
		name: "secrets (from volume source projected) found",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.GetContainer().VolumeMounts = []corev1.VolumeMount{{
					Name:      "asdf1",
					MountPath: "/asdf1",
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.Spec.Volumes = []corev1.Volume{{
					Name: "asdf1",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{{
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "asdf1",
									},
								},
							}},
						},
					},
				}}
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: addSecretToInformer(kubefake.NewSimpleClientset(), "asdf1", "foo")(getSecretInformer()),
	}, {
		name: "secrets (from volume source projected) not found",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.GetContainer().VolumeMounts = []corev1.VolumeMount{{
					Name:      "asdf1",
					MountPath: "/asdf1",
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.Spec.Volumes = []corev1.Volume{{
					Name: "asdf1",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{{
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "asdf1",
									},
								},
							}, {
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "asdf2",
									},
								},
							}},
						},
					},
				}}
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: getSecretInformer(),
		expectedErr: `secret "asdf1" not found: spec.volumes[0].volumeSource.projected.sources[0].secret
secret "asdf2" not found: spec.volumes[0].volumeSource.projected.sources[1].secret`,
	}, {
		name: "secrets (from volume source projected) not found but optional is true",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.GetContainer().VolumeMounts = []corev1.VolumeMount{{
					Name:      "asdf1",
					MountPath: "/asdf1",
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.Spec.Volumes = []corev1.Volume{{
					Name: "asdf1",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{{
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "asdf1",
									},
									Optional: ptr.Bool(true),
								},
							}},
						},
					},
				}}
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: getSecretInformer(),
	}, {
		name: "secrets (from EnvFrom) found",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.Containers = []corev1.Container{{
					EnvFrom: []corev1.EnvFromSource{{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "asdf1",
							},
						},
					}},
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: addSecretToInformer(kubefake.NewSimpleClientset(), "asdf1", "foo")(getSecretInformer()),
	}, {
		name: "secrets (from EnvFrom) not found",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.Containers = []corev1.Container{{
					EnvFrom: []corev1.EnvFromSource{{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "asdf1",
							},
						},
					}, {
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "asdf2",
							},
						},
					}},
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: getSecretInformer(),
		expectedErr: `secret "asdf1" not found: spec.containers[0].envFrom[0].secretRef
secret "asdf2" not found: spec.containers[0].envFrom[1].secretRef`,
	}, {
		name: "secrets (from EnvFrom) not found but optional is true",
		rev: revision(
			func(revision *v1alpha1.Revision) {
				revision.Spec.Containers = []corev1.Container{{
					EnvFrom: []corev1.EnvFromSource{{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "asdf1",
							},
							Optional: ptr.Bool(true),
						},
					}},
				}}
				container(revision.Spec.GetContainer(),
					//withTCPReadinessProbe(),
				)
				revision.ObjectMeta.Labels = map[string]string{}
			},
		),
		si: getSecretInformer(),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifySecrets(test.rev, test.si.Lister())
			if diff := cmp.Diff(test.expectedErr, err.Error()); diff != "" {
				t.Fatalf("verifySecrets (-wantErr, +gotErr) = %v", diff)
			}
		})
	}
}

func addSecretToInformer(fake *kubefake.Clientset, secretName string, namespace string) func(si corev1informer.SecretInformer) corev1informer.SecretInformer {
	return func(si corev1informer.SecretInformer) corev1informer.SecretInformer {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"test-secret": []byte("origin"),
			},
		}
		fake.CoreV1().Secrets(secret.Namespace).Create(secret)
		si.Informer().GetIndexer().Add(secret)
		return si
	}
}

func getSecretInformer() corev1informer.SecretInformer {
	fake := kubefake.NewSimpleClientset()
	informer := k8sinformers.NewSharedInformerFactory(fake, 0)
	return informer.Core().V1().Secrets()
}
