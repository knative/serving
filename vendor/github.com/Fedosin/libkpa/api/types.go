/*
Copyright 2025 The libkpa Authors

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

// Package api contains the API types and interfaces for the KPA autoscaler library.
package api

import (
	"time"
)

// ScalingMetric represents the metric type used for autoscaling decisions.
type ScalingMetric string

const (
	// Concurrency specifies the scaling should be based on in-flight requests.
	Concurrency ScalingMetric = "concurrency"
	// RPS specifies the scaling should be based on requests per second.
	RPS ScalingMetric = "rps"
)

// AutoscalerSpec defines the parameters for autoscaling behavior.
type AutoscalerSpec struct {
	// MaxScaleUpRate is the maximum rate at which the autoscaler will scale up pods.
	// It must be greater than 1.0. For example, a value of 2.0 allows scaling up
	// by at most doubling the pod count. Default is 1000.0.
	MaxScaleUpRate float64

	// MaxScaleDownRate is the maximum rate at which the autoscaler will scale down pods.
	// It must be greater than 1.0. For example, a value of 2.0 allows scaling down
	// by at most halving the pod count. Default is 2.0.
	MaxScaleDownRate float64

	// ScalingMetric is the metric used for scaling decisions (concurrency or rps).
	// Default is Concurrency.
	ScalingMetric ScalingMetric

	// TargetValue is the desired value of the scaling metric per pod that we aim to maintain.
	// For concurrency, this is the number of concurrent requests per pod.
	// For RPS, this is the number of requests per second per pod.
	// This must be less than or equal to TotalValue. Default is 100.0.
	TargetValue float64

	// TotalValue is the total capacity of the scaling metric that a pod can handle.
	// For concurrency, this is the maximum concurrent requests a pod can handle.
	// For RPS, this is the maximum requests per second a pod can handle.
	// Default is 1000.0.
	TotalValue float64

	// TargetBurstCapacity is the desired burst capacity to maintain without queuing.
	// If negative, it means unlimited burst capacity. Default is 211.0.
	TargetBurstCapacity float64

	// PanicThreshold is the threshold for entering panic mode, expressed as a
	// percentage of desired pod count. If the observed load over the panic window
	// exceeds this percentage of the current pod count capacity, panic mode is triggered.
	// Default is 200 (200%).
	PanicThreshold float64

	// PanicWindowPercentage is the percentage of the stable window used for
	// panic mode calculations. Must be in range [1.0, 100.0]. Default is 10.0.
	PanicWindowPercentage float64

	// StableWindow is the time window over which metrics are averaged for
	// scaling decisions. Must be between 5s and 600s. Default is 60s.
	StableWindow time.Duration

	// ScaleDownDelay is the minimum time that must pass at reduced load
	// before scaling down. Default is 0s (immediate scale down).
	ScaleDownDelay time.Duration

	// InitialScale is the initial number of pods to create. Must be >= 0.
	// Default is 1.
	InitialScale int32

	// MinScale is the minimum number of pods to maintain. Must be >= 0.
	// Default is 0 (can scale to zero).
	MinScale int32

	// MaxScale is the maximum number of pods to maintain. 0 means unlimited.
	// Default is 0.
	MaxScale int32

	// ActivationScale is the minimum scale to use when scaling from zero.
	// Must be >= 1. Default is 1.
	ActivationScale int32

	// Reachable indicates whether the service is reachable (has active traffic).
	// This affects scale-down behavior. Default is true.
	Reachable bool
}

// PodMetrics represents metrics collected from a single pod.
type PodMetrics struct {
	// PodName is the name of the pod.
	PodName string

	// Timestamp is when these metrics were collected.
	Timestamp time.Time

	// ConcurrentRequests is the number of in-flight requests.
	ConcurrentRequests float64

	// RequestsPerSecond is the rate of requests.
	RequestsPerSecond float64

	// ProcessUptime is how long the pod has been running.
	ProcessUptime time.Duration
}

// ScaleRecommendation represents the autoscaler's scaling recommendation.
type ScaleRecommendation struct {
	// DesiredPodCount is the recommended number of pods.
	DesiredPodCount int32

	// ExcessBurstCapacity is the difference between spare capacity and
	// configured target burst capacity. Negative values indicate insufficient
	// capacity for the desired burst level.
	ExcessBurstCapacity int32

	// ScaleValid indicates whether the recommendation is valid.
	// False if insufficient data was available.
	ScaleValid bool

	// InPanicMode indicates whether the autoscaler is in panic mode.
	InPanicMode bool

	// ObservedStableValue is the observed metric value over the stable window.
	ObservedStableValue float64

	// ObservedPanicValue is the observed metric value over the panic window.
	ObservedPanicValue float64

	// CurrentPodCount is the current number of ready pods.
	CurrentPodCount int32
}

// Config represents the complete autoscaler configuration.
type Config struct {
	// AutoscalerSpec contains the core autoscaling parameters.
	AutoscalerSpec

	// EnableScaleToZero enables scaling to zero pods. Default is true.
	EnableScaleToZero bool

	// ScaleToZeroGracePeriod is the time to wait before scaling to zero
	// after the service becomes idle. Default is 30s.
	ScaleToZeroGracePeriod time.Duration

	// ContainerConcurrencyTargetFraction is the fraction of container concurrency
	// that should be the autoscaling target. Range (0, 1]. Default is 0.7.
	ContainerConcurrencyTargetFraction float64

	// ContainerConcurrencyTargetDefault is the default target concurrency
	// for containers without an explicit annotation. Default is 100.
	ContainerConcurrencyTargetDefault float64

	// RPSTargetDefault is the default target RPS for containers without
	// an explicit annotation. Default is 200.
	RPSTargetDefault float64

	// TargetUtilization is the target resource utilization fraction. Default is 0.7.
	TargetUtilization float64
}
