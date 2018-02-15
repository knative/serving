package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var statChan = make(chan *types.Stat, 10)
var kubeClient *kubernetes.Clientset
var statSink *websocket.Conn

func connectStatSink() {
	ns := os.Getenv("ELA_NAMESPACE")
	if ns == "" {
		log.Println("No ELA_NAMESPACE provided.")
		return
	}
	log.Println("ELA_NAMESPACE=" + ns)
	rev := os.Getenv("ELA_REVISION")
	if rev == "" {
		log.Println("No ELA_REVISION provided.")
		return
	}
	log.Println("ELA_REVISION=" + rev)

	selector := fmt.Sprintf("revision=%s", rev)
	log.Println("Revision selector: " + selector)
	opt := metav1.ListOptions{
		LabelSelector: selector,
	}

	// TODO(josephburnett): figure out why this errors with "unknown (get pods)"
	pods := kubeClient.Core().Pods(ns)
	wi, err := pods.Watch(opt)
	if err != nil {
		log.Printf("Error watching pods: %v", err)
		return
	}

	services := kubeClient.CoreV1().Services(ns)
	wi, err = services.Watch(opt)
	if err != nil {
		log.Println("Error watching services: %v", err)
		return
	}
	defer wi.Stop()
	ch := wi.ResultChan()
	log.Println("Looking for autoscaler service.")
	for {
		event := <-ch
		if svc, ok := event.Object.(*corev1.Service); ok {
			if svc.Name != rev+"--autoscaler" {
				log.Println("This is not the service you're looking for: " + svc.Name)
				continue
			}
			ip := svc.Spec.ClusterIP
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
		log.Println("Sending stat.")
		if statSink == nil {
			log.Println("Stat sink not connected.")
			continue
		}
		var b bytes.Buffer
		enc := gob.NewEncoder(&b)
		err := enc.Encode(s)
		if err != nil {
			log.Println(err)
			continue
		}
		err = statSink.WriteMessage(websocket.BinaryMessage, b.Bytes())
		if err != nil {
			log.Println(err)
			continue
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Println("Request received.")
	stat := &types.Stat{
		RequestCount: 1,
	}
	statChan <- stat
	target, err := url.Parse("http://localhost:8080")
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	log.Println("Forwarding a request to the app container at ", time.Now().String())
	proxy.ServeHTTP(w, r)
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
	go statReporter()
	defer func() {
		if statSink != nil {
			statSink.Close()
		}
	}()
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8012", nil)
}
