package app

import (
	"github.com/elafros/elafros/sample/autoscale/kdt3/gcs"
	m "github.com/elafros/elafros/sample/autoscale/kdt3/model"
)

func loadGame(gameId, playerId string) (*m.Game, *m.Player, error) {
	game, err := gcs.Load(gameId)
	if err != nil {
		return nil, nil, err
	}
	if playerId == "" {
		return game, nil, nil
	}
	var player *m.Player
	for _, p := range game.Players {
		if p.PlayerId == playerId {
			player = p
		}
	}
	return game, player, nil
}
