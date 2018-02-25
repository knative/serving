package main

import (
	"bytes"
	"encoding/gob"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/elafros/pkg/autoscaler/lib"
	"github.com/google/elafros/pkg/autoscaler/types"

	"github.com/gorilla/websocket"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var kubeClient *kubernetes.Clientset
var statChan = make(chan types.Stat, 100)

func autoscaler() {
	targetConcurrency := float64(10)

	targetConcurrencyParam := os.Getenv("ELA_TARGET_CONCURRENCY")
	if targetConcurrencyParam != "" {
		concurrency, err := strconv.Atoi(targetConcurrencyParam)
		if err != nil {
			panic(err)
		}
		if concurrency > 0 {
			targetConcurrency = float64(concurrency)
		}
	}
	log.Printf("Target concurrency: %0.2f.", targetConcurrency)

	a := lib.NewAutoscaler(targetConcurrency)
	ticker := time.NewTicker(2 * time.Second)

	for {
		select {
		case <-ticker.C:
			scale, ok := a.Scale(time.Now())
			if ok {
				go scaleTo(scale)
			}
		case s := <-statChan:
			a.Record(s, time.Now())
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
	go autoscaler()
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
