package validation

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	kle "knative.dev/pkg/leaderelection"
)

var (
	validComponents = sets.NewString(
		"controller",
		"hpaautoscaler",
		"certcontroller",
		"istiocontroller",
		"nscontroller")
)

func ValidateLeaderElectionConfig(configMap *corev1.ConfigMap) (*kle.Config, error) {
	config, err := kle.NewConfigFromMap(configMap.Data)
	if err != nil {
		return nil, err
	}

	for _, component := range config.EnabledComponents.List() {
		if !validComponents.Has(component) {
			return nil, fmt.Errorf("invalid enabledComponent %q: valid values are %q", component, validComponents.List())
		}
	}

	return config, nil
}
