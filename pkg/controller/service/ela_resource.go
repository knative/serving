package service

import (
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
)

// MakeElaResourceLabels constructs the labels we will apply to Route and Configuration
// resources.
func MakeElaResourceLabels(s *v1alpha1.Service) map[string]string {
	labels := make(map[string]string, len(s.ObjectMeta.Labels)+1)
	labels[ela.ServiceLabelKey] = s.Name

	for k, v := range s.ObjectMeta.Labels {
		labels[k] = v
	}
	return labels
}
