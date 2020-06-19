package ordinal

import (
	"log"
	"os"
	"strconv"
	"strings"
)

const Leader = 1

var (
	Ordinal  int
	IsLeader bool
)

func init() {
	Ordinal = extraOrdinalFromPodName()
	IsLeader = Ordinal == Leader // autoscaler-1 is the one in charging of reconciling
	log.Printf("### ordinal = %d, isleader = %v\n", Ordinal, IsLeader)
}

func extraOrdinalFromPodName() int {
	n := os.Getenv("POD_NAME")
	if i := strings.LastIndex(n, "-"); i != -1 {
		r, err := strconv.ParseUint(n[i+1:], 10, 64)
		if err != nil {
			return 0
		}

		return int(r)
	}

	return 0
}
