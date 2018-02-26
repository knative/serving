package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/google/elafros/pkg/autoscaler/types"

	"github.com/golang/glog"
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
		glog.Error("No ELA_NAMESPACE provided.")
		return
	}
	glog.Versbose("ELA_NAMESPACE=" + ns)
	rev := os.Getenv("ELA_REVISION")
	if rev == "" {
		glog.Error("No ELA_REVISION provided.")
		return
	}
	glog.Verbose("ELA_REVISION=" + rev)

	selector := fmt.Sprintf("revision=%s", rev)
	glog.Verbose("Revision selector: " + selector)
	opt := metav1.ListOptions{
		LabelSelector: selector,
	}

	// TODO(josephburnett): should we use dns instead?
	services := kubeClient.CoreV1().Services(ns)
	wi, err := services.Watch(opt)
	if err != nil {
		glog.Error("Error watching services: %v", err)
		return
	}
	defer wi.Stop()
	ch := wi.ResultChan()
	glog.Info("Looking for autoscaler service.")
	for {
		event := <-ch
		if svc, ok := event.Object.(*corev1.Service); ok {
			if svc.Name != rev+"--autoscaler" {
				glog.Info("This is not the service you're looking for: " + svc.Name)
				continue
			}
			ip := svc.Spec.ClusterIP
			glog.Info("Found autoscaler service %q with IP %q.", svc.Name, ip)
			if ip != "" {
				for {
					time.Sleep(time.Second)
					dialer := &websocket.Dialer{
						HandshakeTimeout: 3 * time.Second,
					}
					conn, _, err := dialer.Dial("ws://"+ip+":8080", nil)
					if err != nil {
						glog.Error(err)
					} else {
						glog.Info("Connected to stat sink.")
						statSink = conn
						return
					}
					glog.Error("Retrying connection to autoscaler.")
				}
			}
		}
	}
}

func statReporter() {
	for {
		s := <-statChan
		glog.Verbose("Sending stat: %+v", s)
		if statSink == nil {
			glog.Error("Stat sink not connected.")
			continue
		}
		var b bytes.Buffer
		enc := gob.NewEncoder(&b)
		err := enc.Encode(s)
		if err != nil {
			glog.Error(err)
			continue
		}
		err = statSink.WriteMessage(websocket.BinaryMessage, b.Bytes())
		if err != nil {
			glog.Error(err)
			statSink = nil
			glog.Error("Attempting reconnection to stat sink.")
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
	glog.Verbose("Request received.")
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
	glog.Verbose("Forwarding a request to the app container at ", time.Now().String())
	proxy.ServeHTTP(w, r)
}

func main() {
	glog.Info("Queue container is running")
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
