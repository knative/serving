package kdt3

import (
        "net/http"
        "net/url"

        "appengine"

        "kdt3/engine"
        m "kdt3/model"
        "kdt3/view"
)

func init() {
        http.HandleFunc("/", getRoot)
        http.HandleFunc("/new", getNew)
        http.HandleFunc("/game", postGame)
        http.HandleFunc("/game/", getGame)
        http.HandleFunc("/move/", postMove)
        http.HandleFunc("/settings/", getSettings)
        http.HandleFunc("/player/", postPlayer)
}

func getRoot(w http.ResponseWriter, r *http.Request) {
        err := view.RootTemplate.Execute(w, nil)
        if internalError(w, err) {
                return
        }
}

func getNew(w http.ResponseWriter, r *http.Request) {
        message := r.FormValue("message")
        err := view.NewGameTemplate.Execute(w, message)
        if internalError(w, err) {
                return
        }
}

func postGame(w http.ResponseWriter, r *http.Request) {
        c := appengine.NewContext(r)
        game, err := createGame(c, r)
        if err != nil {
                http.Redirect(w, r, "/new?message="+url.QueryEscape(err.Error()), http.StatusFound)
                return
        }
        err = view.PostGameTemplate.Execute(w, game)
        if internalError(w, err) {
                return
        }
}

func getGame(w http.ResponseWriter, r *http.Request) {
        c := appengine.NewContext(r)
        gameId := r.URL.Path[len("/game/"):]
        playerId := r.FormValue("player")
        game, viewer, err := loadGame(c, gameId, playerId)
        if internalError(w, err) {
                return
        }
        gameView := view.NewViewableGame(game, viewer)
        gameView.Message = r.FormValue("message")
        tok, err := getToken(c, playerId)
        if err == nil {
                gameView.Token = tok
                gameView.HasToken = true
        }
        err = view.GetGameTemplate.Execute(w, gameView)
        if internalError(w, err) {
                return
        }
}

func postMove(w http.ResponseWriter, r *http.Request) {
        c := appengine.NewContext(r)
        gameId := r.URL.Path[len("/move/"):]
        playerId := r.FormValue("player")
        game, viewer, err := loadGame(c, gameId, playerId)
        if internalError(w, err) {
                return
        }
        if game.Won {
                http.Redirect(w, r, "/game/"+gameId+"?player="+viewer.PlayerId, http.StatusFound)
                return
        }
        point, err := m.ParsePoint(game.Board.K, game.Board.Size, r.FormValue("point"))
        if internalError(w, err) {
                return
        }
        gameMove := &engine.MovableGame{Game: game}
        err = gameMove.Move(viewer.PlayerId, point)
        if err != nil {
                http.Redirect(w, r, "/game/"+gameId+"?player="+viewer.PlayerId+";message="+url.QueryEscape(err.Error()), http.StatusFound)
                return
        }
        gameWin := &engine.WinnableGame{Game: game}
        if gameWin.IsWin() {
                game.Won = true
        } else {
                gameMove.AdvanceTurn()
        }
        err = saveGame(c, gameMove.Game, nil)
        if internalError(w, err) {
                return
        }
        updateClients(c, game)
        http.Redirect(w, r, "/game/"+gameId+"?player="+viewer.PlayerId+";message="+url.QueryEscape("Move accepted."), http.StatusFound)
}

func getSettings(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	gameId := r.URL.Path[len("/settings/"):]
	playerId := r.FormValue("player")
	_, player, err := loadGame(c, gameId, playerId)
	if internalError(w, err) {
		return
	}
	err = view.GetSettingsTemplate.Execute(w, player)
        if internalError(w, err) {
                return
        }
}

func postPlayer(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	gameId := r.URL.Path[len("/player/"):]
	playerId := r.FormValue("player")
	handle := r.FormValue("handle")
	if len(handle) > 20 {
			http.Redirect(w, r, "/game/"+gameId+"?player="+playerId+";message="+url.QueryEscape("Handle cannot be longer than 20 characters."), http.StatusFound)
		return
	}
	game, player, err := loadGame(c, gameId, playerId)
	if internalError(w, err) {
		return
	}
	gamePlayer := game.Players[player.PlayerOrder]
	gamePlayer.Handle = handle
	player.Handle = handle
	err = saveGame(c, game, player)
	if internalError(w, err) {
		return
	}
	http.Redirect(w, r, "/game/"+gameId+"?player="+playerId+";message="+url.QueryEscape("Handle successfully changed."), http.StatusFound)
}

func internalError(w http.ResponseWriter, err error) bool {
        if err != nil {
                http.Error(w, err.Error(), http.StatusInternalServerError)
                return true
        } else {
                return false
        }
}
