package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type request struct {
	w http.ResponseWriter
	r *http.Request
	c chan struct{}
}

var requestChan = make(chan *request)

func init() { log.Println("Init queue container.") }
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
