package builder

import (
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller/configuration/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgotesting "k8s.io/client-go/testing"
)

type ConfigBuilder struct {
	*v1alpha1.Configuration
}

func Config(name, namespace string, generation int64) ConfigBuilder {
	return ConfigBuilder{
		Configuration: &v1alpha1.Configuration{
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
			Status: v1alpha1.ConfigurationStatus{
				Conditions: []v1alpha1.ConfigurationCondition{},
			},
		},
	}
}

func (bldr ConfigBuilder) WithStatus(s v1alpha1.ConfigurationStatus) ConfigBuilder {
	bldr.Configuration.Status = s
	return bldr
}

func (bldr ConfigBuilder) WithBuild(b buildv1alpha1.BuildSpec) ConfigBuilder {
	bldr.Configuration.Spec.Build = &b
	return bldr
}

func (bldr ConfigBuilder) WithObservedGeneration(gen int64) ConfigBuilder {
	bldr.Configuration.Status.ObservedGeneration = gen
	return bldr
}

func (bldr ConfigBuilder) WithLatestCreatedRevisionName(name string) ConfigBuilder {
	bldr.Configuration.Status.LatestCreatedRevisionName = name
	return bldr
}

func (bldr ConfigBuilder) WithLatestReadyRevisionName(name string) ConfigBuilder {
	bldr.Configuration.Status.LatestReadyRevisionName = name
	return bldr
}

func (bldr ConfigBuilder) WithCondition(c v1alpha1.ConfigurationCondition) ConfigBuilder {
	bldr.Configuration.Status.Conditions = append(bldr.Configuration.Status.Conditions, c)
	return bldr
}

func (bldr ConfigBuilder) WithConcurrencyModel(cm v1alpha1.RevisionRequestConcurrencyModelType) ConfigBuilder {
	bldr.Configuration.Spec.RevisionTemplate.Spec.ConcurrencyModel = cm
	return bldr
}

func (bldr ConfigBuilder) Build() *v1alpha1.Configuration {
	return bldr.Configuration
}

func (bldr ConfigBuilder) AsUpdateAction() clientgotesting.UpdateActionImpl {
	action := clientgotesting.UpdateActionImpl{}
	action.Verb = "update"
	action.Object = bldr.Configuration
	return action
}

func (bldr ConfigBuilder) ToRevision() RevisionBuilder {
	return RevisionBuilder{
		Revision: resources.MakeRevision(bldr.Configuration),
	}
}

func (bldr ConfigBuilder) ToBuild() *buildv1alpha1.Build {
	return resources.MakeBuild(bldr.Configuration)
}
