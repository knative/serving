package builder

import (
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ConfigBuilder struct {
	Config *v1alpha1.Configuration
}

func Config(name, namespace string, generation int64) ConfigBuilder {
	return ConfigBuilder{
		Config: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: generation,
				RevisionTemplate: v1alpha1.RevisionTemplateSpec{
					Spec: v1alpha1.RevisionSpec{
						Container: corev1.Container{
							Image: "busybox",
						},
					},
				},
			},
		},
	}
}

func (bldr ConfigBuilder) WithStatus(s v1alpha1.ConfigurationStatus) ConfigBuilder {
	bldr.Config.Status = s
	return bldr
}

func (bldr ConfigBuilder) WithBuild(b buildv1alpha1.BuildSpec) ConfigBuilder {
	bldr.Config.Spec.Build = &b
	return bldr
}

func (bldr ConfigBuilder) WithConcurrencyModel(cm v1alpha1.RevisionRequestConcurrencyModelType) ConfigBuilder {
	bldr.Config.Spec.RevisionTemplate.Spec.ConcurrencyModel = cm
	return bldr
}

func (bldr ConfigBuilder) Build() *v1alpha1.Configuration {
	return bldr.Config
}

func (bldr ConfigBuilder) GetObjectKind() schema.ObjectKind {
	return bldr.Config.GetObjectKind()
}

func (bldr ConfigBuilder) DeepCopyObject() runtime.Object {
	return bldr.Config.DeepCopyObject()
}
