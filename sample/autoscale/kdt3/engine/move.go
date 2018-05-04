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
package engine

import (
        "errors"

        m "kdt3/model"
)

type MovableGame struct {
        *m.Game
}

func (g *MovableGame) Move(playerId string, point m.Point) error {
        if playerId != g.TurnId {
                return errors.New("Invalid move: out of turn.")
        }
        cell := g.Board.D
        for _, v := range point {
                cell = cell.D[v]
        }
        if cell.IsClaimed {
                return errors.New("Invalid move: cell already claimed.")
        }
        cell.IsClaimed = true
        cell.Player = g.TurnOrder
        return nil
}

func (g *MovableGame) AdvanceTurn() {
        g.TurnOrder = (g.TurnOrder + 1) % len(g.Players)
        g.TurnId = g.Players[g.TurnOrder].PlayerId
}
