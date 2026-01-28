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
	"cmp"
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	netheader "knative.dev/networking/pkg/http/header"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/signals"
)

const suffixMessageEnv = "SUFFIX"

var delay time.Duration

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

type handler struct {
	ctx context.Context
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	d := params.Get("delay")
	if d != "" {
		log.Println("Found delay header")
		parsed, _ := strconv.Atoi(d)
		delay = time.Duration(parsed) * time.Second
	}

	if netheader.IsKubeletProbe(r) {
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

	go func() {
		<-h.ctx.Done()
		log.Println("sending close message")
		conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"),
		)
	}()

	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

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
		if delay > 0 {
			time.Sleep(delay)
		}

		if err = conn.WriteMessage(messageType, message); err != nil {
			log.Println("Failed to write message:", err)
			return
		}
		log.Printf("Successfully wrote: %q", message)
	}
}

func main() {
	ctx := signals.NewContext()
	addr := ":" + cmp.Or(os.Getenv("PORT"), "8080")
	server := pkgnet.NewServer(addr, &handler{ctx: ctx})

	log.Println("Listening on port", addr)
	go server.ListenAndServe()

	<-ctx.Done()
	server.Shutdown(context.Background())
}
