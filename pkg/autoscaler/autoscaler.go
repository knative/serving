package main

import (
	"bytes"
	"encoding/gob"
	"log"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"

	"github.com/gorilla/websocket"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	stableWindowSeconds     float64       = 60
	stableWindow            time.Duration = 60 * time.Second
	stableConcurrencyPerPod float64       = 1

	panicWindowSeconds              float64       = 6
	panicWindow                     time.Duration = 6 * time.Second
	panicConcurrencyPerPodThreshold float64       = stableConcurrencyPerPod * 2
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var statChan = make(chan types.Stat, 100)
var kubeClient *kubernetes.Clientset

func autoscaler() {

	// Record QPS per unique observed pod so missing data doesn't skew the results
	var stats = make(map[time.Time]types.Stat)
	var panicTime *time.Time
	var maxPanicPods float64 = 0

	record := func(stat types.Stat) {
		now := time.Now()
		stats[now] = stat
	}

	tick := func() {

		stableTotal := float64(0)
		stableCount := float64(0)
		stablePods := make(map[string]bool)

		panicTotal := float64(0)
		panicCount := float64(0)
		panicPods := make(map[string]bool)

		now := time.Now()
		for sec, stat := range stats {
			if sec.Add(panicWindow).After(now) {
				panicTotal = panicTotal + float64(stat.ConcurrentRequests)
				panicCount = panicCount + 1
				panicPods[stat.PodName] = true
			}
			if sec.Add(stableWindow).After(now) {
				stableTotal = stableTotal + float64(stat.ConcurrentRequests)
				stableCount = stableCount + 1
				stablePods[stat.PodName] = true
			} else {
				delete(stats, sec)
			}
		}

		// Stop panicking after the surge has made its way into the stable metric.
		if panicTime != nil && panicTime.Add(stableWindow).Before(time.Now()) {
			log.Println("Un-panicking.")
			panicTime = nil
			maxPanicPods = 0
		}

		// Do nothing when we have no data.
		if len(stablePods) == 0 {
			log.Println("No data to scale on.")
			return
		}

		observedStableConcurrency := stableTotal / stableCount
		desiredStablePodCount := (observedStableConcurrency/stableConcurrencyPerPod)*float64(len(stablePods)) + 1

		observedPanicConcurrency := panicTotal / panicCount
		desiredPanicPodCount := (observedPanicConcurrency/stableConcurrencyPerPod)*float64(len(panicPods)) + 1

		log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
			observedStableConcurrency, stableWindowSeconds, stableCount, len(stablePods))
		log.Printf("Observed average %0.1f concurrency over %v seconds over %v samples over %v pods.",
			observedPanicConcurrency, panicWindowSeconds, panicCount, len(panicPods))

		// Begin panicking when we cross the short-term QPS per pod threshold.
		if panicTime == nil && len(panicPods) > 0 && observedPanicConcurrency > panicConcurrencyPerPodThreshold {
			log.Println("PANICKING")
			tmp := time.Now()
			panicTime = &tmp
		}

		if panicTime != nil {
			log.Printf("Operating in panic mode.")
			if desiredPanicPodCount > maxPanicPods {
				log.Printf("Continue PANICKING. Increasing pods from %v to %v.", len(panicPods), desiredPanicPodCount)
				tmp := time.Now()
				panicTime = &tmp
				maxPanicPods = desiredPanicPodCount
			}
			go scaleTo(int32(math.Floor(maxPanicPods)))
		} else {
			log.Println("Operating in stable mode.")
			go scaleTo(int32(math.Floor(desiredStablePodCount)))
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

func scaleTo(podCount int32) {
	log.Printf("Target scale is %v", podCount)
	deploymentName := os.Getenv("ELA_DEPLOYMENT")
	ns := os.Getenv("ELA_NAMESPACE")
	dc := kubeClient.ExtensionsV1beta1().Deployments(ns)
	deployment, err := dc.Get(deploymentName, metav1.GetOptions{})
	if err != nil {
		log.Printf("Error getting Deployment %q: %s", deploymentName, err)
		return
	}
	if *deployment.Spec.Replicas == podCount {
		log.Println("Already at scale.")
		return
	}
	deployment.Spec.Replicas = &podCount
	_, err = dc.Update(deployment)
	if err != nil {
		log.Printf("Error updating Deployment %q: %s", deploymentName, err)
	}
	log.Println("Successfully scaled.")
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("New metrics source online.")
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			//log.Println(err)
			log.Println("Metrics source dropping off.")
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
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	kubeClient = kc
	http.HandleFunc("/", handler)
	go autoscaler()
	http.ListenAndServe(":8080", nil)
}
