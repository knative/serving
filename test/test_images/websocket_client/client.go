/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/gorilla/websocket"
	"github.com/knative/serving/test"
)

const (
	ingressIPEnv  = "INGRESS_IP"
	targetHostEnv = "TARGET_HOST"
)

func mustGetEnv(env string) string {
	value := os.Getenv(env)
	if value == "" {
		log.Fatalf("No env %v provided.", env)
	}
	return value
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("Websocket client received request.")
	ingressIP := mustGetEnv(ingressIPEnv)
	targetHost := mustGetEnv(targetHostEnv)
	u := url.URL{Scheme: "ws", Host: ingressIP, Path: "/"}
	var conn *websocket.Conn

	conn, resp, err := websocket.DefaultDialer.Dial(u.String(),
		map[string][]string{
			"Host": []string{targetHost},
		},
	)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		if resp != nil {
			body := new(bytes.Buffer)
			defer resp.Body.Close()
			body.ReadFrom(resp.Body)
			log.Printf("Connection failed: %v.  Response=%+v, ResponseBody=%q", err, resp, body)
		} else {
			log.Printf("Connection failed: %v.", err)
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send a message.
	sent := "Hello, websocket"
	log.Printf("Sending message %q to server.", sent)
	if err = conn.WriteMessage(websocket.TextMessage, []byte(sent)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Attempt to read back the same message.
	if _, recv, err := conn.ReadMessage(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if sent != string(recv) {
		http.Error(w, fmt.Sprintf("Expected to receive back the message: %q but received %q", sent, string(recv)), http.StatusInternalServerError)
		return
	} else {
		fmt.Fprintln(w, recv)
	}
}

func main() {
	flag.Parse()
	log.Print("Websocket client started.")
	test.ListenAndServeGracefully(":8080", handler)
}
