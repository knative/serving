package types

import "time"

type Stat struct {
	// The time the data point was collected on the pod.
	Time *time.Time

	// The unique identity of this pod.  Used to count how many pods
	// are contributing to the metrics.
	PodName string

	// Number of requests currently being handled by this pod.
	ConcurrentRequests int32
}
