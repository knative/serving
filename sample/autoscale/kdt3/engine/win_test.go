package engine

import (
        "reflect"
        "testing"

        m "kdt3/model"
)

func TestWinnableBoardK1(t *testing.T) {
        board := &m.Board{
                K: 2,
                Size: 2,
                D: &m.Cell{
                        D: []*m.Cell{
                                &m.Cell{
                                        D: []*m.Cell{
                                                &m.Cell{IsClaimed: true, Player: 1},
                                                &m.Cell{IsClaimed: true, Player: 2},
                                        },
                                },
                                &m.Cell{
                                        D: []*m.Cell{
                                                &m.Cell{},
                                                &m.Cell{IsClaimed: true, Player: 1},
                                        },
                                },
                        },
                },
        }
        rules := &m.Rules{InARow: 2}
        expect := []m.Segment{
                m.Segment{m.Point{0, 0}, m.Point{1, 1},},
                m.Segment{m.Point{1, 1}, m.Point{0, 0},},
        }
        actual := (&WinnableBoard{board}).GetWins(1, rules)
        if !reflect.DeepEqual(expect, actual) {
                t.Errorf("Expected %v but got %v\n", expect, actual)
        }
}

func TestEachDirection1(t *testing.T) {
        expect := []m.Direction{
                m.Direction{Decline},
                m.Direction{Neutral},
                m.Direction{Incline},
        }
        actual := collect(1)
        if !reflect.DeepEqual(actual, expect) {
                t.Errorf("Expected: %v Actual: %v", expect, actual)
        }
}

func TestEachDirection2(t *testing.T) {
        expect := []m.Direction{
                m.Direction{Decline, Decline},
                m.Direction{Neutral, Decline},
                m.Direction{Incline, Decline},
                m.Direction{Decline, Neutral},
                m.Direction{Neutral, Neutral},
                m.Direction{Incline, Neutral},
                m.Direction{Decline, Incline},
                m.Direction{Neutral, Incline},
                m.Direction{Incline, Incline},
        }
        actual := collect(2)
        if !reflect.DeepEqual(actual, expect) {
                t.Error("Expected: %v Actual: %v", expect, actual)
        }
}

func TestEachDirectionK(t *testing.T) {
        if len(collect(3)) != 27 {
                t.Fail()
        }
        if len(collect(4)) != 81 {
                t.Fail()
        }
        if len(collect(5)) != 243 {
                t.Fail()
        }
}

func collect(K int) []m.Direction {
        actual := make([]m.Direction, 0, 3)
        eachDirection(K, func(direction m.Direction) {
                d := make(m.Direction, len(direction))
                copy(d, direction)
                actual = append(actual, d)
        })
        return actual
}

func TestWinningK1(t *testing.T) {
        K := 1
        player := 1
        root := &m.Cell{
                D: []*m.Cell{
                        &m.Cell{IsClaimed: true, Player: player},
                        &m.Cell{IsClaimed: true, Player: player},
                },
        }
        rules := &m.Rules{InARow: 2}
        if !isWinningVector(K, root, player, rules, m.Point{0}, m.Direction{Incline}) {
                t.Errorf("Expected K1 win.")
        }
        if !isWinningVector(K, root, player, rules, m.Point{1}, m.Direction{Decline}) {
                t.Errorf("Expected K1 win.")
        }
}

func TestNotWinningK1(t *testing.T) {
        K := 1
        player := 1
        root := &m.Cell{
                D: []*m.Cell{
                        &m.Cell{IsClaimed: true, Player: player},
                        &m.Cell{IsClaimed: false},
                },
        }
        rules := &m.Rules{InARow: 2}
        if isWinningVector(K, root, player, rules, m.Point{0}, m.Direction{Incline}) {
                t.Errorf("Expected no K1 win.")
        }
        if isWinningVector(K, root, player, rules, m.Point{1}, m.Direction{Decline}) {
                t.Errorf("Expected no K1 win.")
        }
}

func TestWinningK2Diagonal(t *testing.T) {
        K := 2
        player := 1
        root := &m.Cell{
                D: []*m.Cell{
                        &m.Cell{
                                D: []*m.Cell{
                                        &m.Cell{IsClaimed: true, Player: player},
                                        &m.Cell{IsClaimed: false},
                                },
                        },
                        &m.Cell{
                                D: []*m.Cell{
                                        &m.Cell{IsClaimed: false},
                                        &m.Cell{IsClaimed: true, Player: player},
                                },
                        },
                },
        }
        rules := &m.Rules{InARow: 2}
        if !isWinningVector(K, root, player, rules, m.Point{0,0}, m.Direction{Incline, Incline}) {
                t.Errorf("Expected K2 diagonal win")
        }
        if !isWinningVector(K, root, player, rules, m.Point{1,1}, m.Direction{Decline, Decline}) {
                t.Errorf("Expected K2 diagonal win")
        }
        if isWinningVector(K, root, player, rules, m.Point{1,0}, m.Direction{Decline, Incline}) {
                t.Errorf("Unexpected K2 diagonal win")
        }
        if isWinningVector(K, root, player, rules, m.Point{0,1}, m.Direction{Incline, Decline}) {
                t.Errorf("Unexpected K2 diagonal win")
        }
        if isWinningVector(K, root, player, rules, m.Point{0,0}, m.Direction{Neutral, Decline}) {
                t.Errorf("Unexpected K2 out-of-bounds win")
        }
        if isWinningVector(K, root, player, rules, m.Point{1,1}, m.Direction{Incline, Neutral}) {
                t.Errorf("Unexpected K2 out-of-bounds win")
        }
}

func TestNotWinningK2(t *testing.T) {
        K := 2
        player := 1
        root := &m.Cell{
                D: []*m.Cell{
                        &m.Cell{
                                D: []*m.Cell{
                                        &m.Cell{IsClaimed: true, Player: player},
                                        &m.Cell{IsClaimed: true, Player: player},
                                },
                        },
                        &m.Cell{
                                D: []*m.Cell{
                                        &m.Cell{IsClaimed: true, Player: player},
                                        &m.Cell{IsClaimed: true, Player: player},
                                },
                        },
                },
        }
        rules := &m.Rules{InARow: 2}
        if isWinningVector(K, root, player, rules, m.Point{0,0}, m.Direction{Neutral, Neutral}) {
                t.Errorf("Expected no K2 neutral win")
        }
}

func TestEachPoint(t *testing.T) {
        K := 2
        root := &m.Cell{
                D: []*m.Cell{
                        &m.Cell{
                                D: []*m.Cell{
                                        &m.Cell{},
                                        &m.Cell{},
                                },
                        },
                        &m.Cell{
                                D: []*m.Cell{
                                        &m.Cell{},
                                        &m.Cell{},
                                },
                        },
                },
        }
        expect := []m.Point{
                m.Point{0, 0},
                m.Point{1, 0},
                m.Point{0, 1},
                m.Point{1, 1},
        }
        actual := make([]m.Point, 0)
        eachPoint(K, root, func(p m.Point) {
                point := make(m.Point, len(p))
                copy(point, p)
                actual = append(actual, point)
        })
        if !reflect.DeepEqual(expect, actual) {
                t.Errorf("Expected %v instead of %v\n", expect, actual)
        }
}
