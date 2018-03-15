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
