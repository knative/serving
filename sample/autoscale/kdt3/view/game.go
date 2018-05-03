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
package view

import (
	"html/template"
	"strconv"

	m "github.com/elafros/elafros/sample/autoscale/kdt3/model"
)

type ViewableGame struct {
	*m.Game
	Viewer    *m.Player
	HasViewer bool
	IsMyTurn  bool
	Message   string
	Token     string
	HasToken  bool
}

func NewViewableGame(game *m.Game, viewer *m.Player) *ViewableGame {
	viewableGame := &ViewableGame{
		Game:   game,
		Viewer: viewer,
	}
	if viewer != nil {
		viewableGame.HasViewer = true
		if viewer.PlayerId == game.TurnId {
			viewableGame.IsMyTurn = true
		}
	}
	return viewableGame
}

func (g *ViewableGame) View() template.HTML {
	boardView := &ViewableBoard{
		Board:    g.Board,
		Viewer:   g.Viewer,
		IsMyTurn: g.IsMyTurn,
	}
	return template.HTML(boardView.View())
}

func (g *ViewableGame) PlayerList() template.HTML {
	players := "<ol>"
	for i, p := range g.Players {
		tags := ""
		if g.HasViewer && i == g.TurnOrder {
			tags += " (turn)"
		}
		players += "<li>" + p.Handle + tags + "</li>"
	}
	players += "</ol>"
	return template.HTML(players)
}

type ViewableBoard struct {
	*m.Board
	Viewer   *m.Player
	IsMyTurn bool
}

func (b *ViewableBoard) View() string {
	point := make(m.Point, b.K)
	var recur func(*m.Cell, int) string
	recur = func(c *m.Cell, depth int) string {
		if depth == 0 {
			classes := "cell"
			if c.IsClaimed {
				if c.IsWon {
					if b.Viewer == nil || c.Player == b.Viewer.PlayerOrder {
						classes += " win"
					} else {
						classes += " loss"
					}
				} else if b.Viewer != nil && c.Player == b.Viewer.PlayerOrder {
					classes += " mine"
				} else {
					classes += " yours"
				}
				return "<div class=\"" + classes + "\">" + strconv.Itoa(c.Player+1) + "</div>"
			} else {
				if b.Viewer == nil || !b.IsMyTurn {
					return "<div class=\"" + classes + "\"></div>"
				} else {
					return "<a href=\"/move/" + b.Viewer.GameId + "?player=" + b.Viewer.PlayerId +
						";point=" + point.String() + "\"><div class=\"" + classes + "\"></div></a>"
				}
			}
		} else if depth%2 == 0 {
			table := "<table>"
			for i, v := range c.D {
				point[b.K-depth] = i
				table += "<tr>" + recur(v, depth-1) + "</tr>"
			}
			table += "</table>"
			return table
		} else {
			columns := ""
			for i, v := range c.D {
				point[b.K-depth] = i
				columns += "<td><div class=\"col\">" + recur(v, depth-1) + "</div></td>"
			}
			return columns
		}
	}
	if b.K%2 == 0 {
		return recur(b.D, b.K)
	} else {
		return "<table>" + "<tr>" + recur(b.D, b.K) + "</tr>" + "</table>"
	}
}
