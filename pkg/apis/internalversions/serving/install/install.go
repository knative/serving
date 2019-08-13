package install

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"knative.dev/serving/pkg/apis/internalversions/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

func Install(scheme *runtime.Scheme) {
	utilruntime.Must(serving.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))

	utilruntime.Must(scheme.SetVersionPriority(
		v1beta1.SchemeGroupVersion,
		v1alpha1.SchemeGroupVersion),
	)
}
