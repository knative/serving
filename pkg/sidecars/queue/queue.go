package main

import (
	"bytes"
	"elafros/pkg/autoscaler/types"
	"encoding/gob"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

type request struct {
	w http.ResponseWriter
	r *http.Request
	c chan struct{}
}

var requestChan = make(chan *request)
var statChan = make(chan *types.Stat)

func init() {
	svc := os.GetEnv("ELA_AUTOSCALER_SERVICE")
	if svc == "" {
		log.Println("No ELA_AUTOSCALER_SERVICE provided.")
		return
	}
	// TODO(josephburnett): watch for service.spec.clusterIP
	// go statReporter()
}

func statReporter() {
	conn, _, err := websocket.DefaultDialer.Dial("", nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	for {
		s := <-statChan
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

func consumer() {
	target, err := url.Parse("http://127.0.0.1:8080")
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	for {
		r := <-requestChan
		log.Println("Forwarding a request to the app container at ", time.Now().String())
		proxy.ServeHTTP(r.w, r.r)
		close(r.c)
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
	log.Println("Adding a request to the the request channel at ", time.Now().String())
	requestChan <- req
	<-req.c
}

func main() {
	log.Println("Queue container is running")
	go consumer()
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8012", nil)
}
