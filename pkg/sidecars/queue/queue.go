package main

import (
	"bytes"
	"elafros/pkg/autoscaler/types"
	"encoding/gob"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var statChan = make(chan *types.Stat)
var kubeClient *kubernetes.Clientset
var statSink *websocket.Conn

func connectStatSink() {
	ns := os.GenEnv("ELA_NAMESPACE")
	if ns == "" {
		log.Println("No ELA_NAMESPACE provided.")
		return
	}
	rev := os.GetEnv("ELA_REVISION")
	if rev == "" {
		log.Println("No ELA_REVISION provided.")
		return
	}
	services := kubeClient.CoreV1().Services(ns)
	selector := fmt.Sprintf("revision=%s", rev)
	wi, err := services.Watch(opt)
	if err != nil {
		log.Println(err)
		return
	}
	defer wi.Stop()
	ch := wi.ResultChan()
	for {
		event := <-ch
		if svc, ok := event.Object(corev1.Service); ok {
			ip := svc.ClusterIP
			if ip != "" {
				conn, _, err := websocket.DefaultDialer.Dial(ip, nil)
				if err != nil {
					log.Println(err)
				} else {
					log.Println("Connected to stat sink.")
					statSink = conn
					return
				}
			}
		}
	}
}

func statReporter() {
	for {
		s := <-statChan
		if statSink == nil {
			log.Println("Stat sink not connected.")
			return
		}
		var b bytes.Buffer
		enc := gob.NewEncoder(&b)
		err = enc.Encode(s)
		if err != nil {
			log.Println(err)
			continue
		}
		err = conn.WriteMessage(websocket.BinaryMessage, b.Bytes())
		if err != nil {
			log.Println(err)
			continue
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	stat := &types.Stat{
		RequestCount: 1,
	}
	statChan <- stat
	req := &request{
		w: w,
		r: r,
		c: make(chan struct{}),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	log.Println("Forwarding a request to the app container at ", time.Now().String())
	proxy.ServeHTTP(r.w, r.r)
	close(r.c)
}

func main() {
	log.Println("Queue container is running")
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	kubeClient = kc
	go connectStatSink()
	defer func() {
		if statSink != nil {
			statSink.Close()
		}
	}()
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8012", nil)
}
