/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

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
	targetEnv     = "TARGET"
	hostHeaderEnv = "HOST_HEADER"
)

func mustGetEnv(env string) string {
	value := os.Getenv(env)
	if value == "" {
		log.Fatalf("No env %v provided.", env)
	}
	return value
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("Received a request.")

	target := mustGetEnv(targetEnv)
	hostHeader := mustGetEnv(hostHeaderEnv)
	u := url.URL{Scheme: "ws", Host: target, Path: "/"}
	var conn *websocket.Conn

	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), http.Header{"Host": []string{hostHeader}})
	if err != nil {
		// Cannot connect.
		if resp != nil {
			// If an HTTP error happens we usually have a response body.
			body := new(bytes.Buffer)
			body.ReadFrom(resp.Body)
			resp.Body.Close()
			log.Printf("Connection failed: %v.  Response=%+v, ResponseBody=%q", err, resp, body)
		} else {
			// If a TCP error happens we don't have a response body.
			log.Println("Connection failed:", err)
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("Connection established.")
	defer conn.Close()

	// Send a message.
	sent := "Hello"
	log.Printf("Sending message %q to server.", sent)
	if err = conn.WriteMessage(websocket.TextMessage, []byte(sent)); err != nil {
		log.Println("Error sending message to server:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Send: %q", sent)
	// Attempt to read back the same message.
	if _, recv, err := conn.ReadMessage(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if sent != string(recv) {
		msg := fmt.Sprintf("Expected to receive back the message: %q but received %q", sent, string(recv))
		log.Println(msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	} else {
		fmt.Fprintln(w, string(recv))
	}
}

func main() {
	flag.Parse()
	log.Print("Websocket client started.")
	test.ListenAndServeGracefully(":8080", handler)
}
