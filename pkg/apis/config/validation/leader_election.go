package validation

import (
	"errors"
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
			return nil, errors.New(fmt.Sprintf(""))
		}
	}

	return config, nil
}
