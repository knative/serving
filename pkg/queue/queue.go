package queue

import (
	"time"

	"github.com/elafros/elafros/pkg/autoscaler"
)

type Poke struct{}

type Channels struct {
	ReqInChan        chan Poke             // Ticks with every request arrived
	ReqOutChan       chan Poke             // Ticks with every request completed
	QuantizationChan <-chan time.Time      // Ticks at every quantization interval
	ReportChan       <-chan time.Time      // Ticks with every stat report request
	StatChan         chan *autoscaler.Stat // Stat reporting channel
}

type Stats struct {
	podName string
	ch      Channels
}

func NewStats(podName string, channels Channels) *Stats {
	s := &Stats{
		podName: podName,
		ch:      channels,
	}

	go func() {
		var requestCount int32 = 0
		var concurrentCount int32 = 0
		var bucketedRequestCount int32 = 0
		buckets := make([]int32, 0)
		for {
			select {
			case <-s.ch.ReqInChan:
				requestCount = requestCount + 1
				concurrentCount = concurrentCount + 1
			case <-s.ch.QuantizationChan:
				// Calculate average concurrency for the current
				// 100 ms bucket.
				buckets = append(buckets, concurrentCount)
				// Count the number of requests during bucketed
				// period
				bucketedRequestCount = bucketedRequestCount + requestCount
				requestCount = 0
				// Drain the request out channel only after the
				// bucket statistic has been recorded.  This
				// ensures that very fast requests are not missed
				// by making the smallest granularity of
				// concurrency accounting 100 ms.
			DrainReqOutChan:
				for {
					select {
					case <-s.ch.ReqOutChan:
						concurrentCount = concurrentCount - 1
					default:
						break DrainReqOutChan
					}
				}
			case <-s.ch.ReportChan:
				// Report the average bucket level. Does not
				// include the current bucket.
				var total float64 = 0
				var count float64 = 0
				for _, val := range buckets {
					total = total + float64(val)
					count = count + 1
				}
				now := time.Now()
				stat := &autoscaler.Stat{
					Time:                      &now,
					PodName:                   s.podName,
					AverageConcurrentRequests: total / count,
					RequestCount:              bucketedRequestCount,
				}
				// Send the stat to another process to transmit
				// so we can continue bucketing stats.
				s.ch.StatChan <- stat
				// Reset the stat counts which have been reported.
				bucketedRequestCount = 0
				buckets = make([]int32, 0)
			}
		}
	}()

	return s
}
