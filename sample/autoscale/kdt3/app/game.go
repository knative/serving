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
