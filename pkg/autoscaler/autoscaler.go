package main

import (
	"bytes"
	"encoding/gob"
	"log"
	"net/http"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"

	"github.com/gorilla/websocket"
)

var (
	stableWindowSeconds int           = 60
	stableWindow        time.Duration = stableWindowSeconds * time.Second
	stableQpsPerPod     int           = 10

	panicWindowSeconds int           = 6
	panicWindow        time.Duration = panicWindowSeconds * time.Second
	panicQpsPerPod     int           = 20
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var statChan = make(chan types.Stat, 100)

type podName string

func autoscaler() {

	// Record QPS per unique observed pod so missing data doesn't skew the results
	var qps = make(map[time.Time]map[podName]int)

	record := func(stat types.Stat) {
		second := time.Now().Truncate(time.Second)
		if _, ok := qps[second]; !ok {
			qps[second] = make(map[podName]int)
		}
		queries := qps[second]
		pod := podName(stat.PodName)
		if count, ok := queries[pod]; ok {
			queries[pod] = count + stat.RequestCount
		} else {
			queries[pod] = stat.RequestCount
		}
	}

	tick := func() {

		stableQueries := 0
		stablePods := make(map[podName]bool)

		panicQueries := 0
		panicPods := make(map[podName]bool)

		now := time.Now()
		for sec, queries := range qps {
			if sec.Add(panicWindow).After(now) {
				for pod, count := range queries {
					panicQueries = panicQueries + count
					panicPods[pod] = true
				}
			}
			if sec.Add(stableWindow).After(now) {
				for pod, count := range queries {
					stableQueries = stableQueries + count
					stablePods[pod] = true
				}
			} else {
				delete(qps, sec)
			}
		}

		log.Printf("%v second qps is %v over %v pods.", panicWindowSeconds, panicQueries/panicWindowSeconds, len(panicPods))
		log.Printf("%v second qps %v over %v pods.", stableWindowSeconds, stableQueries/stableWindowSeconds, len(stablePods))

		if len(panicPods) > 0 && vpanicQueries/panicWindowSeconds/len(panicPods) > panicQpsPerPod {
			log.Println("Panicking.")
			scaleTo(panicQueries/panicWindowSeconds/stableQpsPerPod + 1)
		} else if len(stablePods) > 0 {
			scaleTo(stableQueries/stableWindowSeconds/stableQpsPerPod + 1)
		} else {
			log.Println("No data to scale on.")
		}
	}

	ticker := time.NewTicker(2 * time.Second).C

	for {
		select {
		case <-ticker:
			tick()
		case s := <-statChan:
			record(s)
		}
	}
}

func scaleTo(podCount int) {
	log.Printf("Scaling to %v", podCount)
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}
		if messageType != websocket.BinaryMessage {
			log.Println("Dropping non-binary message.")
			continue
		}
		dec := gob.NewDecoder(bytes.NewBuffer(msg))
		var stat types.Stat
		err = dec.Decode(&stat)
		if err != nil {
			log.Println(err)
			continue
		}
		statChan <- stat
	}
}

func main() {
	log.Println("Autoscaler up")
	http.HandleFunc("/", handler)
	go autoscaler()
	http.ListenAndServe(":8080", nil)
}
