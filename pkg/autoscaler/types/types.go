package types

type Stat struct {
	// The unique identity of this pod.  Used to count how many pods
	// are contributing to the metrics.
	PodName string

	// Number of requests currently being handled by this pod.
	ConcurrentRequests int32
}
