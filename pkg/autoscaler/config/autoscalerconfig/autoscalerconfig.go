/*
Copyright 2020 The Knative Authors

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

package autoscalerconfig

import "time"

// Config defines the tunable autoscaler parameters
type Config struct {
	// Feature flags.
	EnableScaleToZero bool

	// Target concurrency knobs for different container concurrency configurations.
	ContainerConcurrencyTargetFraction float64
	ContainerConcurrencyTargetDefault  float64
	// TargetUtilization is used for the metrics other than concurrency. This is not
	// configurable now. Customers can override it by specifying
	// autoscaling.knative.dev/targetUtilizationPercentage in Revision annotation.
	// TODO(yanweiguo): Expose this to config-autoscaler configmap and eventually
	// deprecate ContainerConcurrencyTargetFraction.
	TargetUtilization float64
	// RPSTargetDefault is the default target value for requests per second.
	RPSTargetDefault float64
	// NB: most of our computations are in floats, so this is float to avoid casting.
	TargetBurstCapacity float64

	// ActivatorCapacity is the number of the concurrent requests an activator
	// task can accept. This is used in activator subsetting algorithm, to determine
	// the number of activators per revision.
	ActivatorCapacity float64

	// AllowZeroInitialScale indicates whether InitialScale and
	// autoscaling.internal.knative.dev/initialScale are allowed to be set to 0.
	AllowZeroInitialScale bool

	// InitialScale is the cluster-wide default initial revision size for newly deployed
	// services. This can be set to 0 iff AllowZeroInitialScale is true.
	InitialScale int32

	// MaxScale is the default max scale for any revision created without an
	// autoscaling.knative.dev/maxScale annotation
	MaxScale int32

	// MaxScaleLimit is the maximum allowed MaxScale and `autoscaling.knative.dev/maxScale`
	// annotation value for a revision.
	MaxScaleLimit int32

	// General autoscaler algorithm configuration.
	MaxScaleUpRate           float64
	MaxScaleDownRate         float64
	StableWindow             time.Duration
	PanicWindowPercentage    float64
	PanicThresholdPercentage float64

	// ScaleToZeroGracePeriod is the time we will wait for networking to
	// propagate before scaling down. We may wait less than this if it is safe to
	// do so, for example if the Activator has already been in the path for
	// longer than the window.
	ScaleToZeroGracePeriod time.Duration

	// ScaleToZeroPodRetentionPeriod is the minimum amount of time we will wait
	// before scaling down the last pod.
	ScaleToZeroPodRetentionPeriod time.Duration

	// ScaleDownDelay is the amount of time that must pass at reduced concurrency
	// before a scale-down decision is applied. This can be useful for keeping
	// scaled-up revisions "warm" for a certain period before scaling down. This
	// applies to all scale-down decisions, not just the very last pod.
	// It is independent of ScaleToZeroPodRetentionPeriod, which can be used to
	// add an additional delay to the very last pod, if required.
	ScaleDownDelay time.Duration

	PodAutoscalerClass string
}
