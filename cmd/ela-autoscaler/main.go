package main

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"os"
	"time"

	ela_autoscaler "github.com/google/elafros/pkg/autoscaler"
	"github.com/google/elafros/pkg/autoscaler/types"

	"github.com/golang/glog"
	"github.com/gorilla/websocket"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// The desired number of concurrent requests for each pod.  This
	// is the primary knob for the fast autoscaler which will try
	// achieve a 60-second average concurrency per pod of
	// targetConcurrency.  Another process may tune targetConcurrency
	// to best handle the resource requirements of the revision.
	targetConcurency = float64(1.0)

	// A big enough buffer to handle 1000 pods sending stats every 1
	// second while we do the autoscaling computation (a few hundred
	// milliseconds).
	statBufferSize = 1000
)

var (
	upgrader   = websocket.Upgrader{}
	kubeClient *kubernetes.Clientset
	statChan   = make(chan types.Stat, statBufferSize)
)

func autoscaler() {
	glog.Info("Target concurrency: %0.2f.", targetConcurrency)

	a := ela_autoscaler.NewAutoscaler(targetConcurrency)
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
	glog.Info("Target scale is %v", podCount)
	deploymentName := os.Getenv("ELA_DEPLOYMENT")
	ns := os.Getenv("ELA_NAMESPACE")
	dc := kubeClient.ExtensionsV1beta1().Deployments(ns)
	deployment, err := dc.Get(deploymentName, metav1.GetOptions{})
	if err != nil {
		glog.Error("Error getting Deployment %q: %s", deploymentName, err)
		return
	}
	if *deployment.Spec.Replicas == podCount {
		glog.Info("Already at scale.")
		return
	}
	deployment.Spec.Replicas = &podCount
	_, err = dc.Update(deployment)
	if err != nil {
		glog.Error("Error updating Deployment %q: %s", deploymentName, err)
	}
	glog.Info("Successfully scaled.")
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		glog.Error(err)
		return
	}
	glog.Info("New metrics source online.")
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			glog.Info("Metrics source dropping off.")
			return
		}
		if messageType != websocket.BinaryMessage {
			glog.Error("Dropping non-binary message.")
			continue
		}
		dec := gob.NewDecoder(bytes.NewBuffer(msg))
		var stat types.Stat
		err = dec.Decode(&stat)
		if err != nil {
			glog.Error(err)
			continue
		}
		statChan <- stat
	}
}

func main() {
	glog.Info("Autoscaler up")
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err)
	}
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}
	kubeClient = kc
	go autoscaler()
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
