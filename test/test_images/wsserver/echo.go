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
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/test"
)

const suffixMessageEnv = "SUFFIX"

// Gets the message suffix from envvar. Empty by default.
func messageSuffix() string {
	value := os.Getenv(suffixMessageEnv)
	if value == "" {
		return ""
	}
	return value
}

var upgrader = websocket.Upgrader{
	// Allow any origin, since we are spoofing requests anyway.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handler(w http.ResponseWriter, r *http.Request) {
	if network.IsKubeletProbe(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading websocket:", err)
		return
	}
	defer conn.Close()
	log.Println("Connection upgraded to WebSocket. Entering receive loop.")
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			// We close abnormally, because we're just closing the connection in the client,
			// which is okay. There's no value delaying closure of the connection unnecessarily.
			if websocket.IsCloseError(err, websocket.CloseAbnormalClosure) {
				log.Println("Client disconnected.")
			} else {
				log.Println("Handler exiting on error:", err)
			}
			return
		}
		if suffix := messageSuffix(); suffix != "" {
			respMes := string(message) + " " + suffix
			message = []byte(respMes)
		}

		log.Printf("Successfully received: %q", message)
		if err = conn.WriteMessage(messageType, message); err != nil {
			log.Println("Failed to write message:", err)
			return
		}
		log.Printf("Successfully wrote: %q", message)
	}
}

func main() {
	flag.Parse()
	log.SetFlags(0)
	h := network.NewProbeHandler(http.HandlerFunc(handler))
	test.ListenAndServeGracefully(":"+os.Getenv("PORT"), h.ServeHTTP)
}
