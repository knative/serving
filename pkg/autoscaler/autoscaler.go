package main

import (
	"bytes"
	"encoding/gob"
	"log"
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
	stableWindowSeconds int32         = 60
	stableWindow        time.Duration = 60 * time.Second
	stableQpsPerPod     int32         = 10

	panicWindowSeconds      int32         = 6
	panicWindow             time.Duration = 6 * time.Second
	panicQpsPerPodThreshold int32         = 20
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var statChan = make(chan types.Stat, 100)
var kubeClient *kubernetes.Clientset

type podName string

func autoscaler() {

	// Record QPS per unique observed pod so missing data doesn't skew the results
	var qps = make(map[time.Time]map[podName]int32)
	var panicTime *time.Time
	var maxPanicPods int32

	record := func(stat types.Stat) {
		second := time.Now().Truncate(time.Second)
		if _, ok := qps[second]; !ok {
			qps[second] = make(map[podName]int32)
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

		stableQueries := int32(0)
		stablePods := make(map[podName]bool)

		panicQueries := int32(0)
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

		observedStableQps := stableQueries / stableWindowSeconds
		desiredStablePods := (observedStableQps / stableQpsPerPod) + 1

		observedPanicQps := panicQueries / panicWindowSeconds
		desiredPanicPods := (observedPanicQps / stableQpsPerPod) + 2

		log.Printf("Observed %v QPS total over %v seconds over %v pods.",
			observedStableQps, stableWindowSeconds, len(stablePods))
		log.Printf("Observed %v QPS total over %v seconds over %v pods.",
			observedPanicQps, panicWindowSeconds, len(panicPods))

		// Begin panicking when we cross the short-term QPS per pod threshold.
		if panicTime == nil && len(panicPods) > 0 && observedPanicQps/int32(len(panicPods)) > panicQpsPerPodThreshold {
			log.Println("PANICKING")
			tmp := time.Now()
			panicTime = &tmp
		}

		if panicTime != nil {
			log.Printf("Operating in panic mode.")
			if desiredPanicPods > maxPanicPods {
				log.Printf("Continue PANICKING. Increasing from %v pods to %v pods.", maxPanicPods, desiredPanicPods)
				tmp := time.Now()
				panicTime = &tmp
				maxPanicPods = desiredPanicPods
			}
			if desiredStablePods > maxPanicPods {
				log.Printf("Continue PANICKING. Increasing from %v pods to %v pods.", maxPanicPods, desiredStablePods)
				tmp := time.Now()
				panicTime = &tmp
				maxPanicPods = desiredStablePods
			}
			go scaleTo(maxPanicPods)
		} else {
			log.Println("Operating in stable mode.")
			go scaleTo(desiredStablePods)
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
