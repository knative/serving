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
