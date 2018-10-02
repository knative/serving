package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultTime is 10min
const DefaultTimeout = 10 * time.Minute

// SetDefaults for build
func (b *Build) SetDefaults() {
	if b.Spec.ServiceAccountName == "" {
		b.Spec.ServiceAccountName = "default"
	}
	if b.Spec.Timeout.Duration == 0 {
		b.Spec.Timeout = metav1.Duration{Duration: DefaultTimeout}
	}
}
