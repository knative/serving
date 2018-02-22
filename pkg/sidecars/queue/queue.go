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

var podName string = os.Getenv("ELA_POD")
var statChan = make(chan *types.Stat, 10)
var reqInChan = make(chan struct{}, 10)
var reqOutChan = make(chan struct{}, 10)
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

	// TODO(josephburnett): should we use dns instead?
	services := kubeClient.CoreV1().Services(ns)
	wi, err := services.Watch(opt)
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
			log.Printf("Found autoscaler service %q with IP %q.", svc.Name, ip)
			if ip != "" {
				for {
					time.Sleep(time.Second)
					dialer := &websocket.Dialer{
						HandshakeTimeout: 3 * time.Second,
					}
					conn, _, err := dialer.Dial("ws://"+ip+":8080", nil)
					if err != nil {
						log.Println(err)
					} else {
						log.Println("Connected to stat sink.")
						statSink = conn
						return
					}
					log.Println("Retrying connection to autoscaler.")
				}
			}
		}
	}
}

func statReporter() {
	for {
		s := <-statChan
		log.Printf("Sending stat: %+v", s)
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
			statSink = nil
			log.Println("Attempting reconnection to stat sink.")
			go connectStatSink()
			continue
		}
	}
}

func concurrencyReporter() {
	var concurrentRequests int32 = 0
	ticker := time.NewTicker(time.Second).C
	for {
		select {
		case <-ticker:
			stat := &types.Stat{
				PodName:            podName,
				ConcurrentRequests: concurrentRequests,
			}
			statChan <- stat
		case <-reqInChan:
			concurrentRequests = concurrentRequests + 1
		case <-reqOutChan:
			concurrentRequests = concurrentRequests - 1
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Println("Request received.")
	var in struct{}
	reqInChan <- in
	defer func() {
		var out struct{}
		reqOutChan <- out
	}()
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
	go concurrencyReporter()
	defer func() {
		if statSink != nil {
			statSink.Close()
		}
	}()
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8012", nil)
}
