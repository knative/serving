package model

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"appengine/datastore"
)

type Player struct {
	PlayerOrder int
	PlayerId    string
	GameId      string
	Handle      string
}

type Game struct {
	GameId      string
	Creator     string
	CreatedDate string
	UpdatedDate string
	Players     []*Player
	PlayerCount int
	TurnOrder   int
	TurnId      string
	Won         bool
	Board       *Board
	Rules       *Rules
}

func (g *Game) Load(c <-chan datastore.Property) error {
	var playersStr string
	for property := range c {
		switch {
		case property.Name == "GameId":
			str, ok := property.Value.(string)
			if ok {
				g.GameId = str
			} else {
				return errors.New("GameId is not a string")
			}
		case property.Name == "Creator":
			str, ok := property.Value.(string)
			if ok {
				g.Creator = str
			} else {
				return errors.New("Creator is not a string")
			}
		case property.Name == "CreatedDate":
			str, ok := property.Value.(string)
			if ok {
				g.CreatedDate = str
			} else {
				return errors.New("CreatedDate is not a string")
			}
		case property.Name == "UpdatedDate":
			str, ok := property.Value.(string)
			if ok {
				g.UpdatedDate = str
			} else {
				return errors.New("UpdatedDate is not a string")
			}
		case property.Name == "PlayerCount":
			i, ok := property.Value.(int64)
			if ok {
				g.PlayerCount = int(i)
			} else {
				return errors.New("PlayerCount is not an int")
			}
		case property.Name == "TurnOrder":
			i, ok := property.Value.(int64)
			if ok {
				g.TurnOrder = int(i)
			} else {
				return errors.New("TurnOrder is not an int")
			}
		case property.Name == "TurnId":
			str, ok := property.Value.(string)
			if ok {
				g.TurnId = str
			} else {
				return errors.New("TurnId is not an int")
			}
		case property.Name == "Won":
			b, ok := property.Value.(bool)
			if ok {
				g.Won = b
			} else {
				return errors.New("Won is not a bool")
			}
		case property.Name == "Players":
			str, ok := property.Value.(string)
			if ok {
				playersStr = str
			} else {
				return errors.New("Players is not a []byte")
			}
		case property.Name == "Board":
			str, ok := property.Value.(string)
			if ok {
				board := new(Board)
				json.Unmarshal([]byte(str), board)
				g.Board = board
			} else {
				return errors.New("Board is not a []byte")
			}
		case property.Name == "Rules":
			str, ok := property.Value.(string)
			if ok {
				rules := new(Rules)
				json.Unmarshal([]byte(str), rules)
				g.Rules = rules
			} else {
				return errors.New("Rules is not a []byte")
			}
		default:
			return errors.New("Unknown property name: " + property.Name)
		}
	}
	players := make([]*Player, g.PlayerCount)
	err := json.Unmarshal([]byte(playersStr), &players)
	if err != nil {
		return err
	}
	g.Players = players
	return nil
}

func (g *Game) Save(c chan<- datastore.Property) error {
	defer close(c)
	c <- datastore.Property{
		Name:  "GameId",
		Value: g.GameId,
	}
	c <- datastore.Property{
		Name:  "Creator",
		Value: g.Creator,
	}
	c <- datastore.Property{
		Name:  "CreatedDate",
		Value: g.CreatedDate,
	}
	c <- datastore.Property{
		Name:  "UpdatedDate",
		Value: g.UpdatedDate,
	}
	c <- datastore.Property{
		Name:  "PlayerCount",
		Value: int64(g.PlayerCount),
	}
	c <- datastore.Property{
		Name:  "TurnOrder",
		Value: int64(g.TurnOrder),
	}
	c <- datastore.Property{
		Name:  "TurnId",
		Value: g.TurnId,
	}
	c <- datastore.Property{
		Name:  "Won",
		Value: g.Won,
	}
	players, err := json.Marshal(g.Players)
	if err != nil {
		return err
	}
	c <- datastore.Property{
		Name:    "Players",
		Value:   string(players),
		NoIndex: true,
	}
	board, err := json.Marshal(g.Board)
	if err != nil {
		return err
	}
	c <- datastore.Property{
		Name:    "Board",
		Value:   string(board),
		NoIndex: true,
	}
	rules, err := json.Marshal(g.Rules)
	if err != nil {
		return err
	}
	c <- datastore.Property{
		Name:    "Rules",
		Value:   string(rules),
		NoIndex: true,
	}
	return nil
}

func NewGame(r *http.Request) (*Game, error) {
	playerCount, err := strconv.Atoi(r.FormValue("playerCount"))
	if err != nil {
		return nil, err
	}
	if playerCount < 2 || playerCount > 10 {
		return nil, errors.New("Player Count must be between 2 and 10.")
	}
	K, err := strconv.Atoi(r.FormValue("k"))
	if err != nil {
		return nil, err
	}
	if K < 2 || K > 5 {
		return nil, errors.New("K must be between 2 and 5")
	}
	size, err := strconv.Atoi(r.FormValue("size"))
	if err != nil {
		return nil, err
	}
	if size < 2 || size > 5 {
		return nil, errors.New("Size must be between 2 and 5")
	}
	inARow, err := strconv.Atoi(r.FormValue("inarow"))
	if err != nil {
		return nil, err
	}
	if inARow < 2 || inARow > size {
		return nil, errors.New("In a row must be between 2 and " + strconv.Itoa(size))
	}
	gameId := newId()
	players := make([]*Player, playerCount)
	for i, _ := range players {
		players[i] = &Player{
			PlayerId:    newId(),
			GameId:      gameId,
			PlayerOrder: i,
			Handle:      "Player " + strconv.Itoa(i+1),
		}
	}
	now, err := time.Now().MarshalText()
	if err != nil {
		return nil, err
	}
	game := &Game{
		GameId:      gameId,
		CreatedDate: string(now),
		UpdatedDate: string(now),
		Players:     players,
		PlayerCount: len(players),
		TurnOrder:   0,
		TurnId:      players[0].PlayerId,
		Won:         false,
		Board:       NewBoard(K, size),
		Rules:       &Rules{InARow: inARow},
	}
	return game, nil
}

type Board struct {
	K    int
	D    *Cell
	Size int
}

func NewBoard(K, size int) *Board {
	board := &Board{K: K, Size: size, D: &Cell{}}
	var populate func(*Cell, int)
	populate = func(cell *Cell, depth int) {
		if depth > 0 {
			cell.D = make([]*Cell, size)
			for i, _ := range cell.D {
				cell.D[i] = &Cell{}
				populate(cell.D[i], depth-1)
			}
		}
	}
	populate(board.D, K)
	return board
}

type Cell struct {
	D         []*Cell
	Player    int
	IsClaimed bool
	IsWon     bool
}

type Rules struct {
	InARow int
}

type Direction []int
type Point []int
type Segment []Point

func ParsePoint(K, size int, s string) (Point, error) {
	ss := strings.Split(s, ",")
	if len(ss) != K {
		return nil, errors.New("Invalid point: incorrect number of dimensions.")
	}
	p := make(Point, K)
	for i, v := range ss {
		d, err := strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
		if d < 0 || d > size-1 {
			return nil, errors.New("Invalid point: out of bounds.")
		}
		p[i] = d
	}
	return p, nil
}

func (p Point) Clone() Point {
	point := make(Point, len(p))
	copy(point, p)
	return point
}

func (p Point) String() string {
	ss := make([]string, len(p))
	for i, v := range p {
		ss[i] = strconv.Itoa(v)
	}
	return strings.Join(ss, ",")
}

func NewSegment(p Point, d Direction, length int) Segment {
	segment := make(Segment, length)
	segment[0] = p.Clone()
	for i := 1; i < length; i++ {
		segment[i] = segment[i-1].Clone()
		for j, v := range d {
			segment[i][j] += v
		}
	}
	return segment
}
