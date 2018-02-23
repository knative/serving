package lib

import (
	"testing"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"
)

var (
	now        time.Time
	scale      int32
	tickerChan <-chan time.Time
	statChan   chan types.Stat
)

func now() time.Time {
	return now
}

func scaleTo(s int32) {
	scale = s
}

func runningAutoscaler() *Autoscaler {
	a := NewAutoscaler(now, tickerChan, statChan, scaleTo)
	go a.Run()
	return a
}

func TestStableMode(t *testing.T) {
	a := runningAutoscaler()
}
