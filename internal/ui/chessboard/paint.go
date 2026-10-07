package chessboard

import (
	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/egoist/mygo/ui"
	"math"
	"time"
)

const pieceFamily = "Xiangqi Pieces, STKaiti, serif"

var (
	teal     = ui.Hex("#386B61")
	red      = ui.Hex("#AF4036")
	ink      = ui.Hex("#292D2C")
	green    = ui.Hex("#429969")
	boardInk = ui.Hex("#795733")
)

// Render draws a static board in r, including all overlays, without interaction
// or animation. It is intended for library thumbnails and export previews.
func Render(p *ui.Painter, r ui.Rect, o Options) {
	l := boardLayout(r, o.Bottom)
	paintBoard(p, l, o)
	for _, piece := range o.Pieces {
		if !piece.Square.Valid() {
			continue
		}
		sc := float32(1)
		if o.Selected != nil && *o.Selected == piece.Square {
			sc = 1.055
		}
		paintPiece(p, l.at(screen(piece.Square, o.Bottom)), l.unit*.81*sc, piece, o, 1)
	}
	paintArrow(p, l, o.Recommendation)
}
func circle(q point, d float32) ui.Rect { return ui.Rect{X: q.x - d/2, Y: q.y - d/2, W: d, H: d} }
func line(p *ui.Painter, a, b point, width float32, c ui.Color) {
	var path ui.Path
	path.MoveTo(a.x, a.y)
	path.LineTo(b.x, b.y)
	p.StrokePath(&path, width, c)
}

type textKey struct {
	text string
	size float32
}

type centeredText struct {
	glyphs   []ui.Glyph
	width    float32
	baseline float32
}

// Board labels use their own font metrics. RichText keeps the paragraph's
// theme-sized baseline, which pushes tiny thumbnail labels below their pieces.
// Shape is uncached in mygo, so retain the small, frequently reused labels.
var boardText = make(map[textKey]centeredText)

func centerText(p *ui.Painter, q point, size float32, text string, color ui.Color) {
	key := textKey{text: text, size: size}
	label, ok := boardText[key]
	if !ok {
		font := ui.Font{Family: pieceFamily, Size: size, Weight: 600}
		metrics := font.Metrics()
		label.glyphs = ui.Shape(text, font)
		for _, glyph := range label.glyphs {
			label.width = max(label.width, glyph.X+glyph.Advance)
		}
		label.baseline = (metrics.Ascent - metrics.Descent) / 2
		if len(boardText) >= 256 {
			clear(boardText)
		}
		boardText[key] = label
	}
	p.Glyphs(label.glyphs, q.x-label.width/2, q.y+label.baseline, color)
}
func paintBoard(p *ui.Painter, l layout, o Options) {
	if l.unit <= 0 {
		return
	}
	r, u := l.rect, l.unit
	radius := min(float32(15), u*.4)
	p.FillGradient(r, ui.LinearGradient{From: ui.Hex("#F2DEBA"), To: ui.Hex("#E7C799"), Angle: 145}, radius)
	p.Clip(r, radius, func() {
		for i := 0; i < 70; i++ {
			y := r.Y + float32(i)*r.H/70
			var grain ui.Path
			grain.MoveTo(r.X, y)
			grain.CubeTo(r.X+r.W*.33, y-u*.035, r.X+r.W*.68, y+u*.065, r.X+r.W, y+u*.035)
			p.StrokePath(&grain, max(float32(.45), u*.011), ui.RGBA(255, 255, 255, .09))
		}
	})
	p.Stroke(r, ui.Hex("#C9A577").Alpha(.7), radius, max(float32(.6), u*.016))
	var grid ui.Path
	segment := func(a, b point) { grid.MoveTo(a.x, a.y); grid.LineTo(b.x, b.y) }
	at := func(x, y float32) point { return l.at(point{x, y}) }
	for row := 0; row < 10; row++ {
		segment(at(0, float32(row)), at(8, float32(row)))
	}
	for file := 0; file < 9; file++ {
		x := float32(file)
		if file == 0 || file == 8 {
			segment(at(x, 0), at(x, 9))
		} else {
			segment(at(x, 0), at(x, 4))
			segment(at(x, 5), at(x, 9))
		}
	}
	for _, row := range []float32{0, 7} {
		segment(at(3, row), at(5, row+2))
		segment(at(5, row), at(3, row+2))
	}
	p.StrokePath(&grid, max(float32(.65), u*.014), boardInk.Alpha(.75))
	gap := u * .055
	p.Stroke(ui.Rect{X: l.origin.x - gap, Y: l.origin.y - gap, W: 8*u + 2*gap, H: 9*u + 2*gap}, boardInk.Alpha(.65), u*.02, max(float32(.7), u*.018))
	marks := []point{{1, 2}, {7, 2}, {1, 7}, {7, 7}}
	for _, row := range []float32{3, 6} {
		for _, file := range []float32{0, 2, 4, 6, 8} {
			marks = append(marks, point{file, row})
		}
	}
	var path ui.Path
	for _, m := range marks {
		q := l.at(m)
		for _, dx := range []float32{-1, 1} {
			if (m.x == 0 && dx < 0) || (m.x == 8 && dx > 0) {
				continue
			}
			for _, dy := range []float32{-1, 1} {
				gap, length := u*.065, u*.12
				path.MoveTo(q.x+dx*(gap+length), q.y+dy*gap)
				path.LineTo(q.x+dx*gap, q.y+dy*gap)
				path.LineTo(q.x+dx*gap, q.y+dy*(gap+length))
			}
		}
	}
	p.StrokePath(&path, max(float32(.5), u*.012), boardInk.Alpha(.65))
	left, right := "楚 河", "汉 界"
	if o.Bottom == domain.Black {
		left, right = right, left
	}
	centerText(p, at(2, 4.5), u*.39, left, boardInk.Alpha(.86))
	centerText(p, at(6, 4.5), u*.39, right, boardInk.Alpha(.86))
	if m := o.LastMove; m != nil && m.From.Valid() && m.To.Valid() {
		p.Fill(circle(l.at(screen(m.From, o.Bottom)), u*.23), teal.Alpha(.35), u*.115)
		p.Stroke(circle(l.at(screen(m.To, o.Bottom)), u*.88), teal.Alpha(.65), u*.1, max(float32(.8), u*.025))
	}
	for _, move := range o.Legal {
		if o.Selected == nil || move.From != *o.Selected || !move.To.Valid() {
			continue
		}
		occupied := false
		for _, piece := range o.Pieces {
			if piece.Square == move.To {
				occupied = true
				break
			}
		}
		if !occupied {
			p.Fill(circle(l.at(screen(move.To, o.Bottom)), u*.2), teal.Alpha(.45), u*.1)
		}
	}
}
func lightFor(piece domain.Piece, o Options) ui.Color {
	for _, capture := range o.Captures {
		m, err := domain.ParseMove(capture.Move)
		if err != nil || m.To != piece.Square {
			continue
		}
		side := domain.Side(capture.Side)
		if piece.Side == o.Bottom {
			// Friendly endangered pieces stay red even when a different piece is selected.
			if side != o.Bottom {
				return red
			}
		} else if side == o.Bottom && (o.Selected == nil || m.From == *o.Selected) {
			return green
		}
	}
	return ui.Color{}
}
func paintPiece(p *ui.Painter, q point, d float32, piece domain.Piece, o Options, alpha float32) {
	if d <= 0 || alpha <= 0 {
		return
	}
	color := ink
	if piece.Side == domain.Red {
		color = red
	}
	r := circle(q, d)
	selected := o.Selected != nil && *o.Selected == piece.Square
	landing := false
	for _, m := range o.Legal {
		if o.Selected != nil && m.From == *o.Selected && m.To == piece.Square {
			landing = true
			break
		}
	}
	checked := piece.Kind == domain.King && piece.Side == o.CheckedSide
	blur, offset := d*.05, d*.045
	if selected {
		blur = d * .1
		offset = d * .08
	}
	p.Shadow(r, d/2, 0, offset, blur, 0, ui.RGBA(80, 52, 23, .22*alpha))
	base := r
	base.Y += d * .045
	p.Fill(base, ui.Hex("#B18A57").Alpha(alpha), d/2)
	p.FillGradient(r, ui.LinearGradient{From: ui.Hex("#FFF5DB").Alpha(alpha), To: ui.Hex("#E9CEA2").Alpha(alpha), Angle: 145}, d/2)
	p.Stroke(r, ui.RGBA(255, 255, 255, .6*alpha), d/2, max(float32(.45), d*.035))
	p.Stroke(circle(q, d*.8), color.Alpha(.6*alpha), d*.4, max(float32(.4), d*.025))
	centerText(p, point{q.x, q.y - d*.018}, d*.63, piece.Kind.Glyph(piece.Side), color.Alpha(alpha))
	if selected || landing || checked {
		c := teal
		if checked {
			c = red
		} else if !selected {
			c = c.Alpha(.6)
		}
		p.Stroke(circle(q, d*1.11), c.Alpha(float32(c.A)/255*alpha), d*.555, max(float32(.8), d*.047))
	}
	if light := lightFor(piece, o); light.A != 0 {
		ld := d * .21
		lr := circle(point{q.x + d*.35, q.y - d*.35}, ld)
		p.Fill(lr, light.Alpha(alpha), ld/2)
		p.Stroke(lr, ui.RGBA(255, 255, 255, alpha), ld/2, max(float32(.5), d*.026))
	}
}
func paintSprites(p *ui.Painter, l layout, o Options, sprites []sprite, ghosts []ghost, ptr pointerState, now time.Time) bool {
	active := false
	// Removed pieces are frozen at their last display location, then fade out.
	for _, g := range ghosts {
		t := progress(now, g.at, captureDuration)
		if t >= 1 {
			continue
		}
		active = true
		q, _ := g.sprite.position.value(now)
		sc, _ := g.sprite.scale.value(now)
		ghostOptions := Options{Bottom: o.Bottom}
		paintPiece(p, l.at(q), l.unit*.81*sc, g.sprite.piece, ghostOptions, 1-t)
	}
	draw := func(a sprite) {
		q, moving := a.position.value(now)
		sc, scaling := a.scale.value(now)
		if ptr.dragging && ptr.id == a.piece.ID {
			q = ptr.position
			moving = false
		}
		active = active || moving || scaling
		paintPiece(p, l.at(q), l.unit*.81*sc, a.piece, o, 1)
	}
	// Selected and dragged pieces are over their neighbors.
	for _, a := range sprites {
		if !(ptr.dragging && ptr.id == a.piece.ID) && !(o.Selected != nil && *o.Selected == a.piece.Square) {
			draw(a)
		}
	}
	for _, a := range sprites {
		if !(ptr.dragging && ptr.id == a.piece.ID) && o.Selected != nil && *o.Selected == a.piece.Square {
			draw(a)
		}
	}
	for _, a := range sprites {
		if ptr.dragging && ptr.id == a.piece.ID {
			draw(a)
		}
	}
	return active
}
func paintArrow(p *ui.Painter, l layout, m *domain.Move) {
	if m == nil || !m.From.Valid() || !m.To.Valid() || m.From == m.To || l.unit <= 0 {
		return
	}
	from, to := l.at(screen(m.From, l.bottom)), l.at(screen(m.To, l.bottom))
	angle := math.Atan2(float64(to.y-from.y), float64(to.x-from.x))
	u := l.unit
	end := point{to.x - float32(math.Cos(angle))*u*.22, to.y - float32(math.Sin(angle))*u*.22}
	line(p, from, end, u*.085, teal.Alpha(.82))
	p.Fill(circle(from, u*.085), teal.Alpha(.82), u*.043)
	var tip ui.Path
	tip.MoveTo(end.x, end.y)
	for _, offset := range []float64{-.6, .6} {
		tip.LineTo(end.x-float32(math.Cos(angle+offset))*u*.3, end.y-float32(math.Sin(angle+offset))*u*.3)
	}
	tip.Close()
	p.FillPath(&tip, teal.Alpha(.9))
}
