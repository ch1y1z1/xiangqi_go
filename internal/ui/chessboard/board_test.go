package chessboard

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
	"github.com/egoist/mygo/ui"
)

func square(f, r int) domain.Square { return domain.Square{File: f, Rank: r} }
func fixture() []domain.Piece {
	return []domain.Piece{
		{ID: "rk", Side: domain.Red, Kind: domain.King, Square: square(4, 0)},
		{ID: "bk", Side: domain.Black, Kind: domain.King, Square: square(4, 9)},
		{ID: "rr", Side: domain.Red, Kind: domain.Rook, Square: square(0, 0)},
		{ID: "br", Side: domain.Black, Kind: domain.Rook, Square: square(8, 9)},
		{ID: "rh", Side: domain.Red, Kind: domain.Horse, Square: square(3, 3)},
		{ID: "bp", Side: domain.Black, Kind: domain.Pawn, Square: square(4, 5)},
	}
}
func newBoardTester(s *State, o *Options) (*ui.Tester, func(domain.Square) point) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(31).Children(func() {
			ui.Text(c, "棋盘交互探针").Height(28)
			View(c, s, *o).Key("board").Grow(1)
		})
	}, 620, 720)
	// Includes a nonzero element origin to catch window/local coordinate mistakes.
	at := func(q domain.Square) point {
		return boardLayout(ui.Rect{X: 31, Y: 59, W: 558, H: 630}, o.Bottom).at(screen(q, o.Bottom))
	}
	return tt, at
}
func TestNativeHitFlipResizeAndKeyboard(t *testing.T) {
	s := &State{}
	var taps []domain.Square
	deletes := 0
	selected := square(0, 0)
	o := Options{Pieces: fixture(), Bottom: domain.Red, Selected: &selected, Interactive: true, OnTap: func(q domain.Square) { taps = append(taps, q) }, OnDelete: func() { deletes++ }}
	tt, at := newBoardTester(s, &o)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	for _, q := range []domain.Square{square(0, 0), square(8, 9), square(4, 4)} {
		p := at(q)
		tt.ClickAt(p.x, p.y)
		if len(taps) == 0 {
			r, _ := tt.Find("象棋棋盘")
			t.Fatalf("no tap at %v board=%v pointer=%+v focused=%v", p, r, s.pointer, tt.Focused("象棋棋盘"))
		}
		if taps[len(taps)-1] != q {
			t.Fatalf("hit %v: %v", q, taps)
		}
	}
	n := len(taps)
	tt.ClickAt(10, 10)
	if len(taps) != n {
		t.Fatal("outside tap")
	}
	o.Bottom = domain.Black
	tt.Frame()
	p := at(square(0, 0))
	tt.ClickAt(p.x, p.y)
	if taps[len(taps)-1] != square(0, 0) {
		t.Fatal("flipped corner")
	}
	// Black bottom: screen left increases File, screen down increases Rank.
	tt.Key(0, ui.KeyLeft)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if taps[len(taps)-1] != square(1, 1) {
		t.Fatalf("flipped navigation: %v", taps[len(taps)-1])
	}
	tt.Key(0, ui.KeyDelete)
	if deletes != 1 {
		t.Fatal("selected delete")
	}
	o.Selected = nil
	tt.Frame()
	tt.Key(0, ui.KeyDelete)
	if deletes != 1 {
		t.Fatal("delete without selection")
	}
	tt.SetSize(820, 820)
	l := boardLayout(ui.Rect{X: 31, Y: 59, W: 758, H: 730}, o.Bottom)
	p = l.at(screen(square(8, 9), o.Bottom))
	tt.ClickAt(p.x, p.y)
	if taps[len(taps)-1] != square(8, 9) {
		t.Fatalf("resized corner: %v", taps)
	}
	o.Interactive = false
	tt.Frame()
	n = len(taps)
	tt.ClickAt(p.x, p.y)
	tt.Key(0, ui.KeyEnter)
	if len(taps) != n {
		t.Fatal("noninteractive board dispatched input")
	}
}
func TestNativeDragThresholdCaptureCancel(t *testing.T) {
	s := &State{}
	taps := 0
	var drags []domain.Move
	o := Options{Pieces: fixture(), Bottom: domain.Red, Interactive: true, OnTap: func(domain.Square) { taps++ }, OnDrag: func(a, b domain.Square) { drags = append(drags, domain.Move{From: a, To: b}) }}
	tt, at := newBoardTester(s, &o)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a, b := at(square(0, 0)), at(square(0, 2))
	tt.Press(a.x, a.y)
	tt.Move(a.x+6, a.y)
	tt.Release(a.x+6, a.y)
	if taps != 1 || len(drags) != 0 {
		t.Fatal("sub-threshold movement must click only")
	}
	tt.Press(a.x, a.y)
	tt.Move(b.x, b.y)
	tt.Release(b.x, b.y)
	if taps != 1 || len(drags) != 1 || drags[0] != (domain.Move{From: square(0, 0), To: square(0, 2)}) {
		t.Fatalf("drag dispatched wrong callbacks: %d %v", taps, drags)
	}
	tt.Press(a.x, a.y)
	tt.Move(-40, -40)
	tt.Release(-40, -40)
	if s.pointer.down || len(drags) != 1 || taps != 1 {
		t.Fatal("outside release must cancel")
	}
	tt.Press(a.x, a.y)
	tt.Move(b.x, b.y)
	tt.Key(0, ui.KeyEscape)
	tt.Release(b.x, b.y)
	if s.pointer.down || len(drags) != 1 || taps != 1 {
		t.Fatal("escape must cancel")
	}
	tt.Press(a.x, a.y)
	tt.Move(b.x, b.y)
	tt.SetFocused(false)
	if s.pointer.down || len(drags) != 1 || taps != 1 {
		t.Fatal("blur synthetic release must not commit")
	}
	tt.SetFocused(true)
	// Empty-board movement is not a piece drag, and must not become a click.
	a = at(square(2, 4))
	tt.Press(a.x, a.y)
	tt.Move(b.x, b.y)
	tt.Release(b.x, b.y)
	if len(drags) != 1 || taps != 1 {
		t.Fatal("empty drag dispatched")
	}
}
func TestAnimationInterruptAndLights(t *testing.T) {
	base := time.Unix(100, 0)
	s := &State{}
	o := Options{Pieces: fixture(), Bottom: domain.Red}
	s.sync(o, base, false)
	o.Pieces[2].Square = square(0, 4)
	s.sync(o, base, false)
	halfway := base.Add(90 * time.Millisecond)
	before, _ := s.sprites[2].position.value(halfway)
	o.Pieces[2].Square = square(5, 4)
	s.sync(o, halfway, false)
	after, moving := s.sprites[2].position.value(halfway)
	if before != after || !moving {
		t.Fatalf("interrupted jump: %v -> %v", before, after)
	}
	end, active := s.sprites[2].position.value(halfway.Add(moveDuration))
	if end != screen(square(5, 4), domain.Red) || active {
		t.Fatal("animation did not settle")
	}
	o.Pieces = append(o.Pieces[:3], o.Pieces[4:]...)
	s.sync(o, halfway, false)
	if len(s.ghosts) != 1 {
		t.Fatal("removed piece must fade")
	}
	o.Bottom = domain.Black
	s.sync(o, halfway, true)
	if len(s.ghosts) != 0 {
		t.Fatal("reduce motion retains ghosts")
	}
	for _, a := range s.sprites {
		q, active := a.position.value(halfway)
		if active || q != screen(a.piece.Square, o.Bottom) {
			t.Fatal("reduce motion must snap")
		}
	}
	o = Options{Bottom: domain.Red, Captures: []engine.Capture{{Move: "a0e5", Side: "red"}, {Move: "i9a0", Side: "black"}}}
	pieces := fixture()
	if lightFor(pieces[5], o) != green || lightFor(pieces[2], o) != red {
		t.Fatal("observer lights")
	}
	q := square(3, 3)
	o.Selected = &q
	if lightFor(pieces[5], o).A != 0 || lightFor(pieces[2], o) != red {
		t.Fatal("selection filters green but not red")
	}
	o.Bottom = domain.Black
	o.Selected = nil
	if lightFor(pieces[5], o) != red || lightFor(pieces[2], o) != green {
		t.Fatal("flipped observer lights")
	}
}

// Set CHESSBOARD_PROBES=1 to emit real mygo renders into build/probes.
func TestRenderProbes(t *testing.T) {
	if os.Getenv("CHESSBOARD_PROBES") == "" {
		t.Skip("set CHESSBOARD_PROBES=1 to emit PNG probes")
	}
	dir := filepath.Join("..", "..", "..", "build", "probes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	selected := square(0, 0)
	last := domain.Move{From: square(3, 1), To: square(3, 3)}
	recommendation := domain.Move{From: square(3, 3), To: square(4, 5)}
	o := Options{Pieces: fixture(), Bottom: domain.Red, Selected: &selected, Legal: []domain.Move{{From: selected, To: square(0, 4)}, {From: selected, To: square(4, 5)}}, LastMove: &last, Recommendation: &recommendation, Captures: []engine.Capture{{Move: "a0e5", Side: "red"}, {Move: "i9a0", Side: "black"}}, CheckedSide: domain.Black, Interactive: true}
	s := &State{}
	tt, _ := newBoardTester(s, &o)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	save("chessboard-native-red", tt.Image())
	o.Bottom = domain.Black
	tt.Frame()
	save("chessboard-native-black", tt.Image())
	o.Pieces = domain.InitialPieces()
	o.Selected = nil
	o.Legal = nil
	o.LastMove = nil
	o.Recommendation = nil
	o.Captures = nil
	o.CheckedSide = ""
	o.Bottom = domain.Red
	tt.Frame()
	save("chessboard-native-initial", tt.Image())
	save("chessboard-thumbnails", ui.Render(func(c *ui.Context) {
		ui.Row(c).Fill().Padding(16).Gap(16).Children(func() {
			for _, side := range []domain.Side{domain.Red, domain.Black} {
				oo := o
				oo.Bottom = side
				ui.Box(c).Grow(1).Draw(func(p *ui.Painter, r ui.Rect) { Render(p, r, oo) })
			}
		})
	}, 420, 240, 2))
	// Multiple render passes must leave presentation and business data unchanged.
	old := snapshotOptions(o)
	sprites := append([]sprite(nil), s.sprites...)
	tt.SetScale(2)
	if !reflect.DeepEqual(o, old) { // callbacks are nil in this fixture
		t.Fatal("render mutated options")
	}
	if !reflect.DeepEqual(s.sprites, sprites) {
		t.Fatal("render mutated settled presentation")
	}
}
