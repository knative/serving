package main

import (
	"log"
	"net/http"

	"github.com/elafros/elafros/sample/autoscale/kdt3/app"
)

func main() {
	log.Printf("Serving kdt3 on port 8080.")

	http.HandleFunc("/game/", app.GetGame)
	http.ListenAndServe(":8080", nil)
}
