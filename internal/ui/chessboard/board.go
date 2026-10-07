// Package chessboard provides the native, controlled Xiangqi board. The caller
// owns rules and position changes; State holds only interaction and presentation.
package chessboard

import (
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
	"github.com/egoist/mygo/ui"
)

// Options describes one position. Callbacks execute on the UI thread. Legal
// contains moves for the position; only Selected's destinations are highlighted.
// Captures follows the original bottom-side observer semantics.
type Options struct {
	Pieces         []domain.Piece
	Bottom         domain.Side
	Selected       *domain.Square
	Legal          []domain.Move
	LastMove       *domain.Move
	Recommendation *domain.Move
	Captures       []engine.Capture
	CheckedSide    domain.Side
	Interactive    bool
	OnTap          func(domain.Square)
	OnDrag         func(domain.Square, domain.Square)
	OnDelete       func()
	OnUndo         func()
	OnRedo         func()
	OnPlace        func(Placement, domain.Square)
	OnImageDrop    func(string)
}

// Placement is the inventory value dragged onto an editor board.
type Placement struct {
	Kind domain.Kind
	Side domain.Side
}

// State must persist across frames, with a distinct instance for each board.
// Its zero value is ready for use. It contains no game/rules state.
type State struct {
	sprites             []sprite
	ghosts              []ghost
	initialized         bool
	bottom              domain.Side
	pointer             pointerState
	cursor              domain.Square
	hasCursor, keyboard bool
}

type point struct{ x, y float32 }
type layout struct {
	rect   ui.Rect
	origin point
	unit   float32
	bottom domain.Side
}

func boardLayout(r ui.Rect, bottom domain.Side) layout {
	// A .65-unit border leaves room for edge pieces, rings and their shadows.
	u := max(float32(0), min(r.W/9.3, r.H/10.3))
	w, h := u*9.3, u*10.3
	r = ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
	return layout{r, point{r.X + .65*u, r.Y + .65*u}, u, bottom}
}
func screen(sq domain.Square, bottom domain.Side) point {
	if bottom == domain.Black {
		return point{float32(8 - sq.File), float32(sq.Rank)}
	}
	return point{float32(sq.File), float32(9 - sq.Rank)}
}
func (l layout) at(q point) point { return point{l.origin.x + q.x*l.unit, l.origin.y + q.y*l.unit} }
func (l layout) square(p point) (domain.Square, bool) {
	if l.unit <= 0 {
		return domain.Square{}, false
	}
	f := int(math.Floor(float64((p.x-l.origin.x)/l.unit) + .5))
	row := int(math.Floor(float64((p.y-l.origin.y)/l.unit) + .5))
	q := domain.Square{File: f, Rank: 9 - row}
	if l.bottom == domain.Black {
		q = domain.Square{File: 8 - f, Rank: row}
	}
	return q, q.Valid()
}

// View returns a flexible element suitable for Grow(1), Fill or explicit Size.
// Bounds is deliberately read in the handler, after layout, rather than cached
// in View (where it would still describe the preceding frame).
func View(c *ui.Context, s *State, o Options) *ui.Element {
	if s == nil {
		panic("chessboard.View: persist a non-nil *State")
	}
	o = snapshotOptions(o)
	e := ui.Box(c).MinWidth(0).MinHeight(0).Label("象棋棋盘").FocusRing(false)
	now := c.Now()
	reduced := c.Preferences().ReduceMotion
	if !o.Interactive || (s.initialized && s.bottom != o.Bottom) {
		s.cancelPointer(now, reduced)
	}
	s.sync(o, now, reduced)
	if o.Interactive {
		e.Focusable().HandleInput(func(ev ui.InputEvent) bool {
			r := e.Bounds()
			l := boardLayout(ui.Rect{W: r.W, H: r.H}, o.Bottom)
			return s.input(ev, l, o, e.Focused(), time.Now(), reduced)
		})
	}
	if o.OnImageDrop != nil {
		for _, path := range e.DroppedFiles() {
			if imagePath(path) {
				o.OnImageDrop(path)
				break
			}
		}
	}
	if o.Interactive && o.OnPlace != nil {
		if piece, ok := ui.Drop[Placement](e); ok {
			x, y, _ := e.PointerPosition()
			r := e.Bounds()
			l := boardLayout(ui.Rect{W: r.W, H: r.H}, o.Bottom)
			if square, valid := l.square(point{x, y}); valid {
				o.OnPlace(piece, square)
			}
		}
	}
	if o.Interactive && o.OnDelete != nil {
		e.ContextMenu(func(m *ui.Menu) {
			if m.Item("删除选中棋子").Disabled(o.Selected == nil).Chosen() {
				o.OnDelete()
			}
		})
	}
	// Freeze presentation inputs. Draw may be called repeatedly without View.
	sprites := append([]sprite(nil), s.sprites...)
	ghosts := append([]ghost(nil), s.ghosts...)
	pointer := s.pointer
	cursor, showCursor := s.cursor, o.Interactive && e.Focused() && (s.keyboard || e.FocusVisible())
	if !s.hasCursor {
		cursor = domain.Square{File: 4, Rank: 0}
		if o.Selected != nil {
			cursor = *o.Selected
		}
	}
	drop := o.OnImageDrop != nil && e.FileDragOver()
	if o.OnPlace != nil {
		_, over := ui.DragOver[Placement](e)
		drop = drop || over
	}
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		l := boardLayout(r, o.Bottom)
		paintBoard(p, l, o)
		active := paintSprites(p, l, o, sprites, ghosts, pointer, p.Now())
		paintArrow(p, l, o.Recommendation)
		if showCursor {
			q := l.at(screen(cursor, o.Bottom))
			p.Stroke(circle(q, l.unit*.96), teal.Alpha(.8), l.unit*.15, max(float32(1), l.unit*.025))
		}
		if drop {
			p.Stroke(l.rect, teal, min(float32(15), l.unit*.4), 3)
		}
		if active && !reduced {
			p.AnimationFrame()
		}
	})
	return e
}

func snapshotOptions(o Options) Options {
	o.Pieces = append([]domain.Piece(nil), o.Pieces...)
	o.Legal = append([]domain.Move(nil), o.Legal...)
	o.Captures = append([]engine.Capture(nil), o.Captures...)
	if o.Selected != nil {
		q := *o.Selected
		o.Selected = &q
	}
	if o.LastMove != nil {
		m := *o.LastMove
		o.LastMove = &m
	}
	if o.Recommendation != nil {
		m := *o.Recommendation
		o.Recommendation = &m
	}
	return o
}
func imagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".tif", ".tiff", ".heic", ".heif":
		return true
	}
	return false
}

type pointerState struct {
	down, dragging   bool
	from             domain.Square
	id               string
	press            point
	origin, position point // normalized screen-grid coordinates
}

func (s *State) cancelPointer(now time.Time, reduced bool) {
	if s.pointer.dragging {
		for i := range s.sprites {
			a := &s.sprites[i]
			if a.piece.ID == s.pointer.id {
				a.position = vectorTween{from: s.pointer.position, to: a.position.to, at: now}
				if reduced {
					a.position.from = a.position.to
				}
			}
		}
	}
	s.pointer = pointerState{}
}
func (s *State) input(ev ui.InputEvent, l layout, o Options, focused bool, now time.Time, reduced bool) bool {
	if !o.Interactive {
		s.cancelPointer(now, reduced)
		return false
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 || l.unit <= 0 {
			return false
		}
		q, ok := l.square(point{ev.X, ev.Y})
		if !ok {
			return false
		}
		s.cancelPointer(now, reduced)
		s.pointer = pointerState{down: true, from: q, press: point{ev.X, ev.Y}}
		// Hit the currently displayed piece, including an interrupted animation.
		for i := len(s.sprites) - 1; i >= 0; i-- {
			a := s.sprites[i]
			pos, _ := a.position.value(now)
			center := l.at(pos)
			dx, dy := ev.X-center.x, ev.Y-center.y
			if dx*dx+dy*dy <= l.unit*l.unit*.45*.45 {
				s.pointer.from = a.piece.Square
				s.pointer.id = a.piece.ID
				s.pointer.origin = pos
				s.pointer.position = pos
				break
			}
		}
		s.cursor = s.pointer.from
		s.hasCursor = true
		s.keyboard = false
		return true
	case ui.InputPointerMove:
		if !s.pointer.down {
			return false
		}
		dx, dy := ev.X-s.pointer.press.x, ev.Y-s.pointer.press.y
		if dx*dx+dy*dy >= 49 {
			s.pointer.dragging = true
		}
		if s.pointer.dragging && l.unit > 0 {
			s.pointer.position = point{s.pointer.origin.x + dx/l.unit, s.pointer.origin.y + dy/l.unit}
		}
		return true
	case ui.InputPointerUp:
		if ev.Button != 0 || !s.pointer.down {
			return false
		}
		ptr := s.pointer
		// SurfaceBlur synthesizes a release; Focused() is already false then.
		s.cancelPointer(now, reduced)
		if !focused {
			return true
		}
		q, ok := l.square(point{ev.X, ev.Y})
		if !ok {
			return true
		}
		dx, dy := ev.X-ptr.press.x, ev.Y-ptr.press.y
		drag := ptr.dragging || dx*dx+dy*dy >= 49
		if drag {
			if ptr.id != "" && q != ptr.from && o.OnDrag != nil {
				o.OnDrag(ptr.from, q)
			}
		} else if o.OnTap != nil {
			o.OnTap(ptr.from)
		}
		return true
	case ui.InputKeyDown:
		if ev.Mods != 0 {
			return false
		}
		if ev.Key == ui.KeyEscape {
			s.cancelPointer(now, reduced)
			s.keyboard = false
			return true
		}
		if ev.Key == ui.KeyDelete || ev.Key == ui.KeyBackspace {
			if o.Selected != nil && o.OnDelete != nil {
				s.cancelPointer(now, reduced)
				o.OnDelete()
				return true
			}
			return false
		}
		if ev.Key != ui.KeyLeft && ev.Key != ui.KeyRight && ev.Key != ui.KeyUp && ev.Key != ui.KeyDown && ev.Key != ui.KeyEnter && ev.Key != ui.KeySpace {
			return false
		}
		s.cancelPointer(now, reduced)
		if !s.hasCursor {
			s.cursor = domain.Square{File: 4, Rank: 0}
			if o.Selected != nil {
				s.cursor = *o.Selected
			}
			s.hasCursor = true
		}
		s.keyboard = true
		p := screen(s.cursor, o.Bottom)
		switch ev.Key {
		case ui.KeyLeft:
			p.x--
		case ui.KeyRight:
			p.x++
		case ui.KeyUp:
			p.y--
		case ui.KeyDown:
			p.y++
		case ui.KeyEnter, ui.KeySpace:
			if o.OnTap != nil {
				o.OnTap(s.cursor)
			}
			return true
		}
		p.x = max(float32(0), min(float32(8), p.x))
		p.y = max(float32(0), min(float32(9), p.y))
		s.cursor, _ = l.square(l.at(p))
		return true
	case ui.InputCommand:
		if ev.Text == "undo" && o.OnUndo != nil {
			s.cancelPointer(now, reduced)
			o.OnUndo()
			return true
		}
		if ev.Text == "redo" && o.OnRedo != nil {
			s.cancelPointer(now, reduced)
			o.OnRedo()
			return true
		}
		if ev.Text == "delete" && o.Selected != nil && o.OnDelete != nil {
			s.cancelPointer(now, reduced)
			o.OnDelete()
			return true
		}
	}
	return false
}
