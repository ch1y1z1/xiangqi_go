package domain

import (
	"fmt"
	"slices"
)

// Editor's undo history only contains piece arrays, as in the original app.
// An empty PlacementKind means placement mode is off.
type Editor struct {
	Pieces        []Piece
	Selected      *Square
	PlacementSide Side
	PlacementKind Kind
	undoStack     [][]Piece
	redoStack     [][]Piece
}

func NewEditor(pieces []Piece) *Editor {
	return &Editor{Pieces: append([]Piece{}, pieces...), PlacementSide: Red, PlacementKind: Rook}
}
func (e *Editor) Choose(kind Kind, side Side) {
	e.PlacementSide = side
	e.PlacementKind = kind
	e.Selected = nil
}
func (e *Editor) count(kind Kind, side Side) int {
	n := 0
	for _, p := range e.Pieces {
		if p.Kind == kind && p.Side == side {
			n++
		}
	}
	return n
}
func (e *Editor) occupied(square Square) bool {
	for _, p := range e.Pieces {
		if p.Square == square {
			return true
		}
	}
	return false
}

func (e *Editor) Tap(square Square) error {
	if !square.Valid() {
		return fmt.Errorf("棋位不在棋盘内")
	}
	if e.occupied(square) {
		if e.Selected != nil && *e.Selected == square {
			e.Selected = nil
		} else {
			e.Selected = &square
		}
		e.PlacementKind = ""
		return nil
	}
	if e.Selected != nil {
		return e.Move(*e.Selected, square)
	}
	kind := e.PlacementKind
	if kind == "" {
		return nil
	}
	if kind.Limit() == 0 || (e.PlacementSide != Red && e.PlacementSide != Black) {
		return fmt.Errorf("无效棋子类型或阵营")
	}
	if e.count(kind, e.PlacementSide) >= kind.Limit() {
		return fmt.Errorf("%s%s已全部放置。", e.PlacementSide.Title(), kind.Glyph(e.PlacementSide))
	}
	next := append(append([]Piece{}, e.Pieces...), Piece{NewID(), e.PlacementSide, kind, square})
	e.replace(next, true)
	if e.count(kind, e.PlacementSide) == kind.Limit() {
		e.PlacementKind = ""
	}
	return nil
}
func (e *Editor) Move(from, to Square) error {
	if !from.Valid() || !to.Valid() {
		return fmt.Errorf("棋位不在棋盘内")
	}
	if from == to || !e.occupied(from) {
		return nil
	}
	e.Selected = &from
	e.PlacementKind = ""
	if e.occupied(to) {
		return fmt.Errorf("这个位置已有棋子，请选择空落点。")
	}
	e.Replace(Applying(Move{from, to}, e.Pieces))
	return nil
}
func (e *Editor) DeleteSelected() {
	if e.Selected == nil {
		return
	}
	next := make([]Piece, 0, len(e.Pieces))
	for _, p := range e.Pieces {
		if p.Square != *e.Selected {
			next = append(next, p)
		}
	}
	e.Replace(next)
}
func (e *Editor) Replace(pieces []Piece) { e.replace(pieces, false) }
func (e *Editor) replace(pieces []Piece, keepPlacement bool) {
	if slices.Equal(e.Pieces, pieces) {
		return
	}
	e.undoStack = append(e.undoStack, append([]Piece{}, e.Pieces...))
	e.redoStack = nil
	e.Pieces = append([]Piece{}, pieces...)
	e.Selected = nil
	if !keepPlacement {
		e.PlacementKind = ""
	}
}
func (e *Editor) CanUndo() bool { return len(e.undoStack) > 0 }
func (e *Editor) CanRedo() bool { return len(e.redoStack) > 0 }
func (e *Editor) Undo() {
	if !e.CanUndo() {
		return
	}
	e.redoStack = append(e.redoStack, append([]Piece{}, e.Pieces...))
	e.Pieces = e.undoStack[len(e.undoStack)-1]
	e.undoStack = e.undoStack[:len(e.undoStack)-1]
	e.Selected = nil
	e.PlacementKind = ""
}
func (e *Editor) Redo() {
	if !e.CanRedo() {
		return
	}
	e.undoStack = append(e.undoStack, append([]Piece{}, e.Pieces...))
	e.Pieces = e.redoStack[len(e.redoStack)-1]
	e.redoStack = e.redoStack[:len(e.redoStack)-1]
	e.Selected = nil
	e.PlacementKind = ""
}
