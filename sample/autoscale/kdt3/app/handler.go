/*
Copyright 2018 Google LLC

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
package app

import (
	"log"
	"net/http"

	"github.com/elafros/elafros/sample/autoscale/kdt3/view"
)

func GetGame(w http.ResponseWriter, r *http.Request) {
	gameId := r.URL.Path[len("/game/"):]
	playerId := r.FormValue("player")
	game, viewer, err := loadGame(gameId, playerId)
	if internalError(w, err) {
		return
	}
	gameView := view.NewViewableGame(game, viewer)
	gameView.Message = r.FormValue("message")
	err = view.GetGameTemplate.Execute(w, gameView)
	if internalError(w, err) {
		return
	}
}

func internalError(w http.ResponseWriter, err error) bool {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("Error: %v.", err)
		return true
	} else {
		return false
	}
}
