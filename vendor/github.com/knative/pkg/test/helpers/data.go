package helpers

import (
	"math/rand"
	"strings"
	"sync"
	"time"
)

const (
	letterBytes   = "abcdefghijklmnopqrstuvwxyz"
	randSuffixLen = 8
	sep           = "-"
)

var (
	r        *rand.Rand
	rndMutex *sync.Mutex
	once     sync.Once
)

func initSeed() {
	seed := time.Now().UTC().UnixNano()
	r = rand.New(rand.NewSource(seed))
	rndMutex = &sync.Mutex{}
}

func AppendRandomString(prefix string) string {
	once.Do(initSeed)
	suffix := make([]byte, randSuffixLen)

	rndMutex.Lock()
	defer rndMutex.Unlock()

	for i := range suffix {
		suffix[i] = letterBytes[r.Intn(len(letterBytes))]
	}

	return strings.Join([]string{prefix, string(suffix)}, sep)
}
