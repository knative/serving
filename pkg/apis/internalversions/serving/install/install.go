package install

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"knative.dev/serving/pkg/apis/internalversions/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

type DiscoveryClient interface {
	ServerGroups() (*metav1.APIGroupList, error)
}

func Install(scheme *runtime.Scheme) {
	utilruntime.Must(serving.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	utilruntime.Must(scheme.SetVersionPriority(
		v1beta1.SchemeGroupVersion,
		v1alpha1.SchemeGroupVersion,
	))
}

func DiscoverAndUpdateVersionPriority(d DiscoveryClient, scheme *runtime.Scheme) {
	groups, err := d.ServerGroups()
	if err != nil {
		panic(err)
	}

	versions := map[string]schema.GroupVersion{
		v1alpha1.SchemeGroupVersion.Version: v1alpha1.SchemeGroupVersion,
		v1beta1.SchemeGroupVersion.Version:  v1beta1.SchemeGroupVersion,
	}

	for _, group := range groups.Groups {
		if group.Name != serving.SchemeGroupVersion.Group {
			continue
		}

		version, ok := versions[group.PreferredVersion.Version]
		if !ok {
			panic(fmt.Sprintf("unknown api version: %s", version))
		}

		utilruntime.Must(scheme.SetVersionPriority(version))
	}
}
