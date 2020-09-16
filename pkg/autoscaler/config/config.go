/*
Copyright 2018 The Knative Authors

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

package config

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
)

const (
	// ConfigName is the name of the config map of the autoscaler.
	ConfigName = "config-autoscaler"

	// BucketSize is the size of the buckets of stats we create.
	// NB: if this is more than 1s, we need to average values in the
	// metrics buckets.
	BucketSize = 1 * time.Second

	defaultTargetUtilization = 0.7

	// GroupName is the the public autoscaling group name. This is used for annotations, labels, etc.
	GroupName = "autoscaling.knative.dev"

	// ClassAnnotationKey is the annotation for the explicit class of autoscaler
	// that a particular resource has opted into. For example,
	//   autoscaling.knative.dev/class: foo
	// This uses a different domain because unlike the resource, it is user-facing.
	ClassAnnotationKey = GroupName + "/class"
	// KPA is Knative Horizontal Pod Autoscaler
	KPA = "kpa.autoscaling.knative.dev"
	// HPA is Kubernetes Horizontal Pod Autoscaler
	HPA = "hpa.autoscaling.knative.dev"

	// MinScaleAnnotationKey is the annotation to specify the minimum number of Pods
	// the PodAutoscaler should provision. For example,
	//   autoscaling.knative.dev/minScale: "1"
	MinScaleAnnotationKey = GroupName + "/minScale"
	// MaxScaleAnnotationKey is the annotation to specify the maximum number of Pods
	// the PodAutoscaler should provision. For example,
	//   autoscaling.knative.dev/maxScale: "10"
	MaxScaleAnnotationKey = GroupName + "/maxScale"

	// InitialScaleAnnotationKey is the annotation to specify the initial scale of
	// a revision when a service is initially deployed. This number can be set to 0 iff
	// allow-zero-initial-scale of config-autoscaler is true.
	InitialScaleAnnotationKey = GroupName + "/initialScale"

	// MetricAnnotationKey is the annotation to specify what metric the PodAutoscaler
	// should be scaled on. For example,
	//   autoscaling.knative.dev/metric: cpu
	MetricAnnotationKey = GroupName + "/metric"
	// Concurrency is the number of requests in-flight at any given time.
	Concurrency = "concurrency"
	// CPU is the amount of the requested cpu actually being consumed by the Pod.
	CPU = "cpu"
	// RPS is the requests per second reaching the Pod.
	RPS = "rps"

	// TargetAnnotationKey is the annotation to specify what metric value the
	// PodAutoscaler should attempt to maintain. For example,
	//   autoscaling.knative.dev/metric: cpu
	//   autoscaling.knative.dev/target: "75"   # target 75% cpu utilization
	TargetAnnotationKey = GroupName + "/target"
	// TargetMin is the minimum allowable target.
	// This can be less than 1 due to the fact that with small container
	// concurrencies and small target utilization values this can get
	// below 1.
	TargetMin = 0.01

	// ScaleToZeroPodRetentionPeriodKey is the annotation to specify the minimum
	// time duration the last pod will not be scaled down, after autoscaler has
	// made the decision to scale to 0.
	// This is the per-revision setting compliment to the
	// scale-to-zero-pod-retention-period global setting.
	ScaleToZeroPodRetentionPeriodKey = GroupName + "/scaleToZeroPodRetentionPeriod"

	// WindowAnnotationKey is the annotation to specify the time
	// interval over which to calculate the average metric.  Larger
	// values result in more smoothing. For example,
	//   autoscaling.knative.dev/metric: concurrency
	//   autoscaling.knative.dev/window: "2m"
	// Only the kpa.autoscaling.knative.dev class autoscaler supports
	// the window annotation.
	WindowAnnotationKey = GroupName + "/window"
	// WindowMin is the minimum allowable stable autoscaling
	// window. KPA-class autoscalers calculate the desired replica
	// count every 2 seconds (tick-interval in config-autoscaler) so
	// the closer the window gets to that value, the more likely data
	// points will be missed entirely by the panic window which is
	// smaller than the stable window. Anything less than 6 seconds
	// isn't going to work well.
	WindowMin = 6 * time.Second
	// WindowMax is the maximum permitted stable autoscaling window.
	// This keeps the event horizon to a reasonable enough limit.
	WindowMax = 1 * time.Hour

	// TargetUtilizationPercentageKey is the annotation which specifies the
	// desired target resource utilization for the revision.
	// TargetUtilization is a percentage in the 1 <= TU <= 100 range.
	// This annotation takes precedence over the config map value.
	TargetUtilizationPercentageKey = GroupName + "/targetUtilizationPercentage"

	// TargetBurstCapacityKey specifies the desired burst capacity for the
	// revision. Possible values are:
	// -1 -- infinite;
	//  0 -- no TBC;
	// >0 -- actual TBC.
	// <0 && != -1 -- an error.
	TargetBurstCapacityKey = GroupName + "/targetBurstCapacity"

	// PanicWindowPercentageAnnotationKey is the annotation to
	// specify the time interval over which to calculate the average
	// metric during a spike. Where a spike is defined as the metric
	// reaching panic level within the panic window (e.g. panic
	// mode). Lower values make panic mode more sensitive. Note:
	// Panic threshold can be overridden with the
	// PanicThresholdPercentageAnnotationKey. For example,
	//   autoscaling.knative.dev/panicWindowPercentage: "5.0"
	//   autoscaling.knative.dev/panicThresholdPercentage: "150.0"
	// Only the kpa.autoscaling.knative.dev class autoscaler supports
	// the panicWindowPercentage annotation.
	// Panic window is specified as a percentage to maintain the
	// autoscaler's algorithm behavior when only the stable window is
	// specified. The panic window will change along with the stable
	// window at the default percentage.
	PanicWindowPercentageAnnotationKey = GroupName + "/panicWindowPercentage"
	// PanicWindowPercentageMin is the minimum allowable panic window
	// percentage. The autoscaler calculates desired replicas every 2
	// seconds (tick-interval in config-autoscaler), so a panic
	// window less than 2 seconds will be missing data points. One
	// percent is a very small ratio and would require a stable
	// window of at least 3.4 minutes. Anything less doesn't make
	// sense.
	PanicWindowPercentageMin = 1.0
	// PanicWindowPercentageMax is the maximum allowable panic window
	// percentage. The KPA autoscaler's panic feature allows the
	// autoscaler to be more responsive over a smaller time scale
	// when necessary. So the panic window cannot be larger than the
	// stable window.
	PanicWindowPercentageMax = 100.0

	// PanicThresholdPercentageAnnotationKey is the annotation to specify
	// the level at what level panic mode will engage when reached within
	// in the panic window. The level is defined as a percentage of
	// the metric target. Lower values make panic mode more
	// sensitive. For example,
	//   autoscaling.knative.dev/panicWindowPercentage: "5.0"
	//   autoscaling.knative.dev/panicThresholdPercentage: "150.0"
	// Only the kpa.autoscaling.knative.dev class autoscaler supports
	// the panicThresholdPercentage annotation
	PanicThresholdPercentageAnnotationKey = GroupName + "/panicThresholdPercentage"
	// PanicThresholdPercentageMin is the minimum allowable panic
	// threshold percentage. The KPA autoscaler's panic feature
	// allows the autoscaler to be more responsive over a smaller
	// time scale when necessary. To prevent flapping, during panic
	// mode the autoscaler never decreases the number of replicas. If
	// the panic threshold was as small as the stable target, the
	// autoscaler would always be panicking and the autoscaler would
	// never scale down. One hundred and ten percent is about the
	// smallest useful value.
	PanicThresholdPercentageMin = 110.0

	// PanicThresholdPercentageMax is the counterpart to the PanicThresholdPercentageMin
	// but bounding from above.
	PanicThresholdPercentageMax = 1000.0
)

// Config defines the tunable autoscaler parameters
// +k8s:deepcopy-gen=true
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

func defaultConfig() *Config {
	return &Config{
		EnableScaleToZero:                  true,
		ContainerConcurrencyTargetFraction: defaultTargetUtilization,
		ContainerConcurrencyTargetDefault:  100,
		// TODO(#1956): Tune target usage based on empirical data.
		TargetUtilization:             defaultTargetUtilization,
		RPSTargetDefault:              200,
		MaxScaleUpRate:                1000,
		MaxScaleDownRate:              2,
		TargetBurstCapacity:           200,
		PanicWindowPercentage:         10,
		ActivatorCapacity:             100,
		PanicThresholdPercentage:      200,
		StableWindow:                  60 * time.Second,
		ScaleToZeroGracePeriod:        30 * time.Second,
		ScaleToZeroPodRetentionPeriod: 0 * time.Second,
		ScaleDownDelay:                0 * time.Second,
		PodAutoscalerClass:            KPA,
		AllowZeroInitialScale:         false,
		InitialScale:                  1,
		MaxScale:                      0,
		MaxScaleLimit:                 0,
	}
}

// NewConfigFromMap creates a Config from the supplied map
func NewConfigFromMap(data map[string]string) (*Config, error) {
	lc := defaultConfig()

	if err := cm.Parse(data,
		cm.AsString("pod-autoscaler-class", &lc.PodAutoscalerClass),

		cm.AsBool("enable-scale-to-zero", &lc.EnableScaleToZero),
		cm.AsBool("allow-zero-initial-scale", &lc.AllowZeroInitialScale),

		cm.AsFloat64("max-scale-up-rate", &lc.MaxScaleUpRate),
		cm.AsFloat64("max-scale-down-rate", &lc.MaxScaleDownRate),
		cm.AsFloat64("container-concurrency-target-percentage", &lc.ContainerConcurrencyTargetFraction),
		cm.AsFloat64("container-concurrency-target-default", &lc.ContainerConcurrencyTargetDefault),
		cm.AsFloat64("requests-per-second-target-default", &lc.RPSTargetDefault),
		cm.AsFloat64("target-burst-capacity", &lc.TargetBurstCapacity),
		cm.AsFloat64("panic-window-percentage", &lc.PanicWindowPercentage),
		cm.AsFloat64("activator-capacity", &lc.ActivatorCapacity),
		cm.AsFloat64("panic-threshold-percentage", &lc.PanicThresholdPercentage),

		cm.AsInt32("initial-scale", &lc.InitialScale),
		cm.AsInt32("max-scale", &lc.MaxScale),
		cm.AsInt32("max-scale-limit", &lc.MaxScaleLimit),

		cm.AsDuration("stable-window", &lc.StableWindow),
		cm.AsDuration("scale-down-delay", &lc.ScaleDownDelay),
		cm.AsDuration("scale-to-zero-grace-period", &lc.ScaleToZeroGracePeriod),
		cm.AsDuration("scale-to-zero-pod-retention-period", &lc.ScaleToZeroPodRetentionPeriod),
	); err != nil {
		return nil, fmt.Errorf("failed to parse data: %w", err)
	}

	// Adjust % ⇒ fractions: for legacy reasons we allow values in the
	// (0, 1] interval, so minimal percentage must be greater than 1.0.
	// Internally we want to have fractions, since otherwise we'll have
	// to perform division on each computation.
	if lc.ContainerConcurrencyTargetFraction > 1.0 {
		lc.ContainerConcurrencyTargetFraction /= 100.0
	}

	return validate(lc)
}

func validate(lc *Config) (*Config, error) {
	if lc.ScaleToZeroGracePeriod < WindowMin {
		return nil, fmt.Errorf("scale-to-zero-grace-period must be at least %v, was: %v", WindowMin, lc.ScaleToZeroGracePeriod)
	}

	if lc.ScaleDownDelay < 0 {
		return nil, fmt.Errorf("scale-down-delay cannot be negative, was: %v", lc.ScaleDownDelay)
	}

	if lc.ScaleDownDelay.Round(time.Second) != lc.ScaleDownDelay {
		return nil, fmt.Errorf("scale-down-delay = %v, must be specified with at most second precision", lc.ScaleDownDelay)
	}

	if lc.ScaleToZeroPodRetentionPeriod < 0 {
		return nil, fmt.Errorf("scale-to-zero-pod-retention-period cannot be negative, was: %v", lc.ScaleToZeroPodRetentionPeriod)
	}

	if lc.TargetBurstCapacity < 0 && lc.TargetBurstCapacity != -1 {
		return nil, fmt.Errorf("target-burst-capacity must be either non-negative or -1 (for unlimited), was: %f", lc.TargetBurstCapacity)
	}

	if lc.ContainerConcurrencyTargetFraction <= 0 || lc.ContainerConcurrencyTargetFraction > 1 {
		return nil, fmt.Errorf("container-concurrency-target-percentage = %f is outside of valid range of (0, 100]", lc.ContainerConcurrencyTargetFraction)
	}

	if x := lc.ContainerConcurrencyTargetFraction * lc.ContainerConcurrencyTargetDefault; x < TargetMin {
		return nil, fmt.Errorf("container-concurrency-target-percentage and container-concurrency-target-default yield target concurrency of %v, can't be less than %v", x, TargetMin)
	}

	if lc.RPSTargetDefault < TargetMin {
		return nil, fmt.Errorf("requests-per-second-target-default must be at least %v, was: %v", TargetMin, lc.RPSTargetDefault)
	}

	if lc.ActivatorCapacity < 1 {
		return nil, fmt.Errorf("activator-capacity = %v, must be at least 1", lc.ActivatorCapacity)
	}

	if lc.MaxScaleUpRate <= 1.0 {
		return nil, fmt.Errorf("max-scale-up-rate = %v, must be greater than 1.0", lc.MaxScaleUpRate)
	}

	if lc.MaxScaleDownRate <= 1.0 {
		return nil, fmt.Errorf("max-scale-down-rate = %v, must be greater than 1.0", lc.MaxScaleDownRate)
	}

	// We can't permit stable window be less than our aggregation window for correctness.
	// Or too big, so that our desisions are too imprecise.
	if lc.StableWindow < WindowMin || lc.StableWindow > WindowMax {
		return nil, fmt.Errorf("stable-window = %v, must be in [%v; %v] range", lc.StableWindow,
			WindowMin, WindowMax)
	}

	if lc.StableWindow.Round(time.Second) != lc.StableWindow {
		return nil, fmt.Errorf("stable-window = %v, must be specified with at most second precision", lc.StableWindow)
	}

	// We ensure BucketSize in the `MakeMetric`, so just ensure percentage is in the correct region.
	if lc.PanicWindowPercentage < PanicWindowPercentageMin ||
		lc.PanicWindowPercentage > PanicWindowPercentageMax {
		return nil, fmt.Errorf("panic-window-percentage = %v, must be in [%v, %v] interval",
			lc.PanicWindowPercentage, PanicWindowPercentageMin, PanicWindowPercentageMax)

	}

	if lc.InitialScale < 0 || (lc.InitialScale == 0 && !lc.AllowZeroInitialScale) {
		return nil, fmt.Errorf("initial-scale = %v, must be at least 0 (or at least 1 when allow-zero-initial-scale is false)", lc.InitialScale)
	}

	if lc.MaxScale < 0 || (lc.MaxScaleLimit > 0 && lc.MaxScale > lc.MaxScaleLimit) {
		return nil, fmt.Errorf("max-scale = %v, must be in [0, max-scale-limit] range", lc.MaxScale)
	}

	if lc.MaxScaleLimit < 0 {
		return nil, fmt.Errorf("max-scale-limit = %v, must be at least 0", lc.MaxScaleLimit)
	}
	return lc, nil
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}
