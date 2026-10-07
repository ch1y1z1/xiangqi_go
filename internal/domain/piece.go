// Package domain contains the Swift-compatible study model. Rule legality is
// deliberately left to the engine; this package also represents incomplete setups.
package domain

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
)

type Side string

const (
	Red   Side = "red"
	Black Side = "black"
)

func (s Side) Opponent() Side {
	if s == Red {
		return Black
	}
	return Red
}
func (s Side) Title() string {
	if s == Red {
		return "红方"
	}
	return "黑方"
}

type Kind string

const (
	King     Kind = "king"
	Advisor  Kind = "advisor"
	Elephant Kind = "elephant"
	Horse    Kind = "horse"
	Rook     Kind = "rook"
	Cannon   Kind = "cannon"
	Pawn     Kind = "pawn"
)

var Kinds = []Kind{King, Advisor, Elephant, Horse, Rook, Cannon, Pawn}

func (k Kind) Limit() int {
	switch k {
	case King:
		return 1
	case Pawn:
		return 5
	case Advisor, Elephant, Horse, Rook, Cannon:
		return 2
	}
	return 0
}

func (k Kind) Glyph(s Side) string {
	switch k {
	case King:
		if s == Red {
			return "帅"
		}
		return "将"
	case Advisor:
		if s == Red {
			return "仕"
		}
		return "士"
	case Elephant:
		if s == Red {
			return "相"
		}
		return "象"
	case Horse:
		return "马"
	case Rook:
		return "车"
	case Cannon:
		return "炮"
	case Pawn:
		if s == Red {
			return "兵"
		}
		return "卒"
	}
	return ""
}

func (k Kind) fen() string {
	switch k {
	case King:
		return "k"
	case Advisor:
		return "a"
	case Elephant:
		return "b"
	case Horse:
		return "n"
	case Rook:
		return "r"
	case Cannon:
		return "c"
	case Pawn:
		return "p"
	}
	return ""
}

// Rank zero is always Red's home rank, regardless of board orientation.
type Square struct {
	File int `json:"file"`
	Rank int `json:"rank"`
}

func (s Square) Valid() bool { return s.File >= 0 && s.File < 9 && s.Rank >= 0 && s.Rank < 10 }
func (s Square) UCI() string { return string(rune('a'+s.File)) + strconv.Itoa(s.Rank) }
func ParseSquare(s string) (Square, error) {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'i' || s[1] < '0' || s[1] > '9' {
		return Square{}, fmt.Errorf("无效棋位：%q", s)
	}
	return Square{int(s[0] - 'a'), int(s[1] - '0')}, nil
}

type Piece struct {
	ID     string `json:"id"`
	Side   Side   `json:"side"`
	Kind   Kind   `json:"kind"`
	Square Square `json:"square"`
}

type Move struct {
	From Square `json:"from"`
	To   Square `json:"to"`
}

func (m Move) UCI() string { return m.From.UCI() + m.To.UCI() }
func ParseMove(s string) (Move, error) {
	if len(s) == 4 {
		from, e1 := ParseSquare(s[:2])
		to, e2 := ParseSquare(s[2:])
		if e1 == nil && e2 == nil && from != to {
			return Move{from, to}, nil
		}
	}
	return Move{}, fmt.Errorf("无效着法：%q", s)
}

// NewID returns an RFC 4122 version 4 UUID in Foundation's uppercase format.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]))
}

// Applying moves a piece without changing its ID, removing any captured piece.
// It does not check rule legality. The input slice is never modified.
func Applying(m Move, pieces []Piece) []Piece {
	result := make([]Piece, 0, len(pieces))
	found := false
	for _, p := range pieces {
		if p.Square == m.To {
			continue
		}
		if !found && p.Square == m.From {
			p.Square = m.To
			found = true
		}
		result = append(result, p)
	}
	return result
}
