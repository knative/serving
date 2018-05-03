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
package model

import (
	"errors"
	"strconv"
	"strings"
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

type Board struct {
	K    int
	D    *Cell
	Size int
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
