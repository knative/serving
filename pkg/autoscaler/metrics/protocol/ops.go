package protocol

// add adds the stats from `src` to `dst`.
func (dst *Stat) Add(src Stat) {
	dst.AverageConcurrentRequests += src.AverageConcurrentRequests
	dst.AverageProxiedConcurrentRequests += src.AverageProxiedConcurrentRequests
	dst.RequestCount += src.RequestCount
	dst.ProxiedRequestCount += src.ProxiedRequestCount
}

// average reduces the aggregate stat from `sample` pods to an averaged one over
// `total` pods.
// The method performs no checks on the data, i.e. that sample is > 0.
//
// Assumption: A particular pod can stand for other pods, i.e. other pods
// have similar concurrency and QPS.
//
// Hide the actual pods behind scraper and send only one stat for all the
// customer pods per scraping. The pod name is set to a unique value, i.e.
// scraperPodName so in autoscaler all stats are either from activator or
// scraper.
func (dst *Stat) Average(sample, total float64) {
	dst.AverageConcurrentRequests = dst.AverageConcurrentRequests / sample * total
	dst.AverageProxiedConcurrentRequests = dst.AverageProxiedConcurrentRequests / sample * total
	dst.RequestCount = dst.RequestCount / sample * total
	dst.ProxiedRequestCount = dst.ProxiedRequestCount / sample * total
}
