package ui

import native "github.com/egoist/mygo/ui"

var (
	paper           = native.Hex("#FAF9F6")
	card            = native.Hex("#FFFFFF")
	ink             = native.Hex("#292D2C")
	muted           = native.Hex("#68635D")
	teal            = native.Hex("#386B61")
	red             = native.Hex("#A64035")
	line            = native.Hex("#DEDFE2")
	sidebar         = native.Hex("#EFEFF2")
	sidebarHover    = native.Hex("#E5E6E9")
	sidebarSelected = native.Hex("#DDE3E0")
)

var theme = func() native.Theme {
	t := *native.LightTheme()
	t.Background, t.Surface, t.Border = paper, card, line
	t.SurfaceHover, t.SurfacePressed = native.Hex("#F0F0ED"), native.Hex("#E7E8E5")
	t.Text, t.TextMuted = ink, muted
	t.Accent, t.AccentHover, t.AccentPressed = teal, native.Hex("#2F6056"), native.Hex("#244E46")
	t.AccentText, t.Danger, t.Success, t.Focus = native.Hex("#FFFFFF"), red, native.Hex("#429969"), teal
	t.FontSize, t.Radius = 14, 9
	return t
}()

func action(c *native.Context, label string, primary, disabled bool, fn func()) *native.Element {
	var e *native.Element
	if primary {
		e = native.PrimaryButton(c, "")
	} else {
		e = native.Button(c, "")
	}
	e.Children(func() { native.Text(c, label).FillWidth().TextAlign(native.Center).SingleLine() })
	e.MinHeight(38).Disabled(disabled)
	if e.Clicked() && fn != nil {
		fn()
	}
	return e
}

// A compact overflow button has its own icon: MenuButton adds a chevron and
// default padding that cannot fit beside an ellipsis in a narrow library row.
func moreMenu(c *native.Context, label string, build func(*native.Menu)) *native.Element {
	e := native.ButtonBase(c).Size(32, 32).Shrink(0).Radius(6).Label(label).Tooltip(label)
	if e.Pressed() {
		e.Background(theme.SurfacePressed)
	} else if e.Hovered() {
		e.Background(theme.SurfaceHover)
	}
	e.Draw(func(p *native.Painter, r native.Rect) {
		for _, dx := range []float32{-5, 0, 5} {
			p.Fill(native.Rect{X: r.X + r.W/2 + dx - 1.5, Y: r.Y + r.H/2 - 1.5, W: 3, H: 3}, muted, 1.5)
		}
	})
	return e.Menu(build)
}

func section(c *native.Context, text string) {
	native.Text(c, text).FontSize(13).FontWeight(600).TextColor(muted).Margin(12, 0, 6)
}

func rule(c *native.Context) { native.Box(c).FillWidth().Height(1).Background(line).Margin(8, 0) }
