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

package autoscaling

import "time"

const (
	domain = ".knative.dev"

	// InternalGroupName is the internal autoscaling group name. This is used for CRDs.
	InternalGroupName = "autoscaling.internal.knative.dev"

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

	// ScaleDownDelayAnnotationKey is the annotation to specify a scale down delay.
	ScaleDownDelayAnnotationKey = GroupName + "/scaleDownDelay"

	// MetricAnnotationKey is the annotation to specify what metric the PodAutoscaler
	// should be scaled on. For example,
	//   autoscaling.knative.dev/metric: cpu
	MetricAnnotationKey = GroupName + "/metric"
	// Concurrency is the number of requests in-flight at any given time.
	Concurrency = "concurrency"
	// CPU is the amount of the requested cpu actually being consumed by the Pod.
	CPU = "cpu"
	// Memory is the amount of the requested memory actually being consumed by the Pod.
	Memory = "memory"
	// RPS is the requests per second reaching the Pod.
	RPS = "rps"

	// TargetAnnotationKey is the annotation to specify what metric value the
	// PodAutoscaler should attempt to maintain. For example,
	//   autoscaling.knative.dev/metric: cpu
	//   autoscaling.knative.dev/target: "75"   # target 75% cpu utilization
	// Or
	//   autoscaling.knative.dev/metric: memory
	//   autoscaling.knative.dev/target: "100"   # target 100MiB memory usage
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

	// MetricAggregationAlgorithmKey is the annotation that can be used for selection
	// of the algorithm to use for averaging metric data in the Autoscaler.
	// Since autoscalers are a pluggable concept, this field is only validated
	// for Revisions that are owned by Knative Pod Autoscaler.
	// The algorithm will apply to both panic and stagble windows.
	// NB: this is an Alpha feature and can be removed or modified
	//     at any point.
	// Possible values for KPA are:
	// - empty/missing or "linear" — linear average over the whole
	//   metric window (default);
	// - weightedExponential — weighted average with exponential decay.
	//   KPA will compute the decay multiplier automatically based on the window size
	//   and it is at least 0.2. This algorithm might not utilize all the values
	//   in the window, due to their coefficients being infinitesimal.
	MetricAggregationAlgorithmKey = GroupName + "/metricAggregationAlgorithm"
	// MetricAggregationAlgorithmLinear is the linear aggregation algorithm with all weights
	// equal to 1.
	MetricAggregationAlgorithmLinear = "linear"
	// MetricAggregationAlgorithmWeightedExponential is the weighted aggregation algorithm
	// with exponentially decaying weights.
	MetricAggregationAlgorithmWeightedExponential = "weightedExponential"

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
