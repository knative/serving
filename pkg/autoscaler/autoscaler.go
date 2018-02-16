package main

import (
	"bytes"
	"encoding/gob"
	"log"
	"net/http"

	"github.com/google/elafros/pkg/autoscaler/types"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}
		if messageType != websocket.BinaryMessage {
			log.Println("Dropping non-binary message.")
			continue
		}
		dec := gob.NewDecoder(bytes.NewBuffer(msg))
		var stat types.Stat
		err = dec.Decode(&stat)
		if err != nil {
			log.Println(err)
			continue
		}
		log.Printf("Stat: %+v", stat)
	}
}

func main() {
	log.Println("Autoscaler up")
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
