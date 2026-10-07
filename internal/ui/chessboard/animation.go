package chessboard

import (
	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"time"
)

const moveDuration = 230 * time.Millisecond
const selectDuration = 120 * time.Millisecond
const captureDuration = 210 * time.Millisecond

type vectorTween struct {
	from, to point
	at       time.Time
}
type scalarTween struct {
	from, to float32
	at       time.Time
}
type sprite struct {
	piece    domain.Piece
	position vectorTween
	scale    scalarTween
}
type ghost struct {
	sprite sprite
	at     time.Time
}

func progress(now, at time.Time, d time.Duration) float32 {
	return max(float32(0), min(float32(1), float32(now.Sub(at))/float32(d)))
}
func ease(t float32) float32 { v := 1 - t; return 1 - v*v*v }
func (a vectorTween) value(now time.Time) (point, bool) {
	t := progress(now, a.at, moveDuration)
	v := ease(t)
	return point{a.from.x + (a.to.x-a.from.x)*v, a.from.y + (a.to.y-a.from.y)*v}, t < 1 && a.from != a.to
}
func (a scalarTween) value(now time.Time) (float32, bool) {
	t := progress(now, a.at, selectDuration)
	return a.from + (a.to-a.from)*ease(t), t < 1 && a.from != a.to
}
func (s *State) sync(o Options, now time.Time, reduced bool) {
	old := make(map[string]sprite, len(s.sprites))
	for _, a := range s.sprites {
		old[a.piece.ID] = a
	}
	live := make(map[string]bool, len(o.Pieces))
	next := make([]sprite, 0, len(o.Pieces))
	for _, piece := range o.Pieces {
		if !piece.Square.Valid() {
			continue
		}
		live[piece.ID] = true
		target := screen(piece.Square, o.Bottom)
		scale := float32(1)
		if o.Selected != nil && *o.Selected == piece.Square {
			scale = 1.055
		}
		a, ok := old[piece.ID]
		if !ok {
			a = sprite{piece: piece, position: vectorTween{from: target, to: target, at: now}, scale: scalarTween{from: 1, to: scale, at: now}}
		}
		if a.position.to != target {
			v, _ := a.position.value(now)
			a.position = vectorTween{from: v, to: target, at: now}
		}
		if a.scale.to != scale {
			v, _ := a.scale.value(now)
			a.scale = scalarTween{from: v, to: scale, at: now}
		}
		a.piece = piece
		if reduced || !s.initialized {
			a.position.from = a.position.to
			a.scale.from = a.scale.to
		}
		next = append(next, a)
	}
	ghosts := make([]ghost, 0, len(s.ghosts)+len(s.sprites))
	if !reduced {
		for _, g := range s.ghosts {
			if !live[g.sprite.piece.ID] && progress(now, g.at, captureDuration) < 1 {
				ghosts = append(ghosts, g)
			}
		}
		for _, a := range s.sprites {
			if !live[a.piece.ID] {
				pos, _ := a.position.value(now)
				sc, _ := a.scale.value(now)
				a.position = vectorTween{from: pos, to: pos, at: now}
				a.scale = scalarTween{from: sc, to: sc, at: now}
				ghosts = append(ghosts, ghost{a, now})
			}
		}
	}
	s.sprites = next
	s.ghosts = ghosts
	s.initialized = true
	s.bottom = o.Bottom
	if s.pointer.down && s.pointer.id != "" {
		if !live[s.pointer.id] {
			s.pointer = pointerState{}
		} else {
			for _, a := range next {
				if a.piece.ID == s.pointer.id && a.piece.Square != s.pointer.from {
					s.cancelPointer(now, reduced)
					break
				}
			}
		}
	}
}
