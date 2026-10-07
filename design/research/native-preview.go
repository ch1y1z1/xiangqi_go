// Design-only native rendering probe. Buttons, scores and data are illustrative;
// this program has no product interaction, engine, network or storage behavior.
package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
)

var paper = ui.Hex("#F7F3EC")
var card = ui.Hex("#FFFCF7")
var ink = ui.Hex("#292D2C")
var muted = ui.Hex("#68635D")
var teal = ui.Hex("#386B61")
var red = ui.Hex("#A64035")
var line = ui.Hex("#E7DFD2")
var gridInk = ui.Hex("#7C5B37")

func txt(p *ui.Painter, x, y, size float32, s string, col ui.Color) {
	p.RichText(x, y, 0, ui.Span{Text: s, Size: size, Color: col})
}
func center(p *ui.Painter, x, y, w, h, size float32, s string, col ui.Color, family string) {
	span := ui.Span{Text: s, Size: size, Color: col, Font: family}
	tw, th := p.MeasureText(0, span)
	p.RichText(x+(w-tw)/2, y+(h-th)/2, 0, span)
}
func box(p *ui.Painter, x, y, w, h float32, bg ui.Color) {
	p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, bg, 12)
}
func button(p *ui.Painter, x, y, w, h float32, s string, primary bool) {
	col := ink
	bg := card
	if primary {
		bg = teal
		col = ui.Hex("#FFFFFF")
	}
	box(p, x, y, w, h, bg)
	if !primary {
		p.Stroke(ui.Rect{X: x, Y: y, W: w, H: h}, line, 10, 1)
	}
	center(p, x, y, w, h, 14, s, col, "")
}
func pathline(p *ui.Painter, x0, y0, x1, y1, w float32, col ui.Color) {
	var path ui.Path
	path.MoveTo(x0, y0)
	path.LineTo(x1, y1)
	p.StrokePath(&path, w, col)
}

type piece struct {
	file, rank int
	label      string
	black      bool
}

var example = []piece{{4, 9, "将", true}, {4, 5, "卒", true}, {3, 3, "马", false}, {0, 1, "车", false}, {4, 0, "帅", false}}

func board(p *ui.Painter, x, y, w, h float32, arrow bool) {
	r := ui.Rect{X: x, Y: y, W: w, H: h}
	p.Shadow(r, 16, 0, 5, 18, 0, ui.RGBA(80, 52, 23, 0.09))
	p.FillGradient(r, ui.LinearGradient{From: ui.Hex("#F2DEBA"), To: ui.Hex("#E7C799"), Angle: 155}, 16)
	p.Stroke(r, ui.Hex("#C9A577"), 16, 1)
	padding := min(float32(26), w*0.065)
	u := min((w-2*padding)/8, (h-2*padding)/9)
	ox := x + (w-8*u)/2
	oy := y + (h-9*u)/2
	for i := 0; i < 60; i++ {
		gy := y + 10 + float32(i)*(h-20)/60
		pathline(p, x+10, gy, x+w-10, gy+1, 0.7, ui.RGBA(255, 255, 255, 0.07))
	}
	for rank := 0; rank < 10; rank++ {
		gy := oy + float32(rank)*u
		pathline(p, ox, gy, ox+8*u, gy, 0.9, gridInk.Alpha(0.62))
	}
	for f := 0; f < 9; f++ {
		gx := ox + float32(f)*u
		if f == 0 || f == 8 {
			pathline(p, gx, oy, gx, oy+9*u, 0.9, gridInk.Alpha(0.62))
		} else {
			pathline(p, gx, oy, gx, oy+4*u, 0.9, gridInk.Alpha(0.62))
			pathline(p, gx, oy+5*u, gx, oy+9*u, 0.9, gridInk.Alpha(0.62))
		}
	}
	for _, rank := range []int{0, 7} {
		pathline(p, ox+3*u, oy+float32(rank)*u, ox+5*u, oy+float32(rank+2)*u, 1, gridInk.Alpha(0.62))
		pathline(p, ox+5*u, oy+float32(rank)*u, ox+3*u, oy+float32(rank+2)*u, 1, gridInk.Alpha(0.62))
	}
	p.Stroke(ui.Rect{X: ox - 4, Y: oy - 4, W: 8*u + 8, H: 9*u + 8}, gridInk.Alpha(0.65), 1, 1)
	if u > 15 {
		for _, mark := range [][2]int{{1, 2}, {7, 2}, {1, 7}, {7, 7}, {0, 3}, {2, 3}, {4, 3}, {6, 3}, {8, 3}, {0, 6}, {2, 6}, {4, 6}, {6, 6}, {8, 6}} {
			cx := ox + float32(mark[0])*u
			cy := oy + float32(mark[1])*u
			gap := u * 0.065
			length := u * 0.12
			for _, dx := range []float32{-1, 1} {
				if mark[0] == 0 && dx < 0 || mark[0] == 8 && dx > 0 {
					continue
				}
				for _, dy := range []float32{-1, 1} {
					var m ui.Path
					m.MoveTo(cx+dx*(gap+length), cy+dy*gap)
					m.LineTo(cx+dx*gap, cy+dy*gap)
					m.LineTo(cx+dx*gap, cy+dy*(gap+length))
					p.StrokePath(&m, 0.7, gridInk.Alpha(0.55))
				}
			}
		}
	}

	if u > 15 {
		center(p, ox+u, oy+4*u, 2*u, u, u*0.34, "楚 河", gridInk, "STKaiti")
		center(p, ox+5*u, oy+4*u, 2*u, u, u*0.34, "汉 界", gridInk, "STKaiti")
	}
	for _, q := range example {
		cx := ox + float32(q.file)*u
		cy := oy + float32(9-q.rank)*u
		d := u * 0.81
		col := red
		if q.black {
			col = ink
		}
		pr := ui.Rect{X: cx - d/2, Y: cy - d/2, W: d, H: d}
		p.Shadow(pr, d/2, 0, 3, 5, 0, ui.RGBA(80, 52, 23, 0.19))
		p.Fill(ui.Rect{X: pr.X, Y: pr.Y + 3, W: d, H: d}, ui.Hex("#B18A57"), d/2)
		p.FillGradient(pr, ui.LinearGradient{From: ui.Hex("#FFF5DB"), To: ui.Hex("#E9CEA2"), Angle: 155}, d/2)
		p.Stroke(pr, ui.RGBA(255, 255, 255, 0.7), d/2, max(float32(0.5), d*0.04))
		inset := d * 0.1
		p.Stroke(ui.Rect{X: pr.X + inset, Y: pr.Y + inset, W: d - 2*inset, H: d - 2*inset}, col.Alpha(0.62), (d-2*inset)/2, max(float32(0.4), d*0.025))
		center(p, pr.X, pr.Y-1, d, d, d*0.62, q.label, col, "STKaiti")
		if arrow && q.black && q.label == "卒" {
			p.Fill(ui.Rect{X: cx + d*0.27, Y: cy - d*0.49, W: 10, H: 10}, ui.Hex("#429969"), 5)
			p.Stroke(ui.Rect{X: cx + d*0.27, Y: cy - d*0.49, W: 10, H: 10}, card, 5, 1.5)
		}
	}
	if arrow {
		sy := oy + 8*u
		pathline(p, ox, sy, ox+3.7*u, sy, 4, teal.Alpha(0.7))
		var tip ui.Path
		tip.MoveTo(ox+4*u, sy)
		tip.LineTo(ox+3.68*u, sy-8)
		tip.LineTo(ox+3.68*u, sy+8)
		tip.Close()
		p.FillPath(&tip, teal.Alpha(0.8))
	}
}
func library(p *ui.Painter) {
	txt(p, 24, 68, 20, "我的残局", ink)
	txt(p, 24, 103, 12, "3 个 · 本地保存", muted)
	button(p, 24, 135, 204, 38, "＋ 新建残局", true)
	names := []string{"车马练习", "从开局开始", "单车研究"}
	details := []string{"继续研究 · 1 处分支", "红方先行 · 32 枚棋子", "红方先行 · 3 枚棋子"}
	for i, name := range names {
		y := float32(200 + i*118)
		if i == 0 {
			box(p, 12, y-10, 228, 106, teal.Alpha(0.07))
		}
		board(p, 24, y, 64, 72, false)
		txt(p, 100, y+5, 16, name, ink)
		txt(p, 100, y+36, 11, details[i], muted)
		txt(p, 213, y+65, 16, "···", teal)
	}
	p.Line(24, 792, 228, 792, 1, line)
	txt(p, 24, 813, 13, "图片识别：无进行中的任务", muted)
	button(p, 24, 860, 94, 36, "设置", false)
	button(p, 130, 860, 98, 36, "从图片新建", false)
}
func study(p *ui.Painter) {
	txt(p, 282, 70, 24, "车马练习", ink)
	txt(p, 282, 105, 13, "红方行棋  ·  双方手动", muted)
	txt(p, 875, 79, 16, "红 +1.20", teal)
	txt(p, 875, 107, 11, "评分示意", muted)
	board(p, 380, 153, 554, 612, true)
	txt(p, 386, 786, 12, "●", ui.Hex("#429969"))
	txt(p, 399, 786, 12, "可安全吃", muted)
	txt(p, 472, 786, 12, "●", red)
	txt(p, 485, 786, 12, "有被吃风险", muted)
	txt(p, 846, 786, 12, "红方视角", muted)
	button(p, 383, 825, 86, 42, "起点", false)
	button(p, 480, 825, 86, 42, "回退", false)
	button(p, 577, 825, 86, 42, "前进", false)
	button(p, 694, 825, 123, 42, "采用建议", true)
	button(p, 828, 825, 108, 42, "双方手动", false)
	txt(p, 1104, 71, 19, "研究面板", ink)
	txt(p, 1104, 106, 12, "当前线路", muted)
	box(p, 1104, 136, 304, 48, teal.Alpha(0.06))
	txt(p, 1120, 150, 14, "起点", teal)
	txt(p, 1104, 207, 13, "同一分叉点 · 下一手比较", muted)
	for i, s := range []string{"车九平五", "马六进四"} {
		y := float32(243 + i*86)
		box(p, 1104, y, 304, 72, card)
		txt(p, 1120, y+12, 16, s, ink)
		score := []string{"红 +1.20", "红 +0.86"}[i]
		txt(p, 1296, y+14, 14, score, teal)
		txt(p, 1120, y+43, 11, []string{"本组较优 · 示例", "相差 0.34 · 示例"}[i], muted)
	}
	p.Line(1104, 441, 1408, 441, 1, line)
	txt(p, 1104, 466, 16, "AI 建议", ink)
	box(p, 1104, 502, 304, 141, teal.Alpha(0.07))
	txt(p, 1120, 522, 23, "车九平五", teal)
	txt(p, 1120, 561, 13, "红 +1.20  ·  标准 1.0 秒", muted)
	button(p, 1120, 595, 160, 32, "查看参考变化", false)
	txt(p, 1104, 684, 13, "当前局面评分   开", ink)
	txt(p, 1104, 719, 13, "分支比较评分   开", ink)
	txt(p, 1104, 765, 12, "快速 0.3 s   标准 1 s   深入 3 s", muted)
	button(p, 1104, 820, 304, 42, "重新比较", false)
}
func editor(p *ui.Painter) {
	txt(p, 282, 70, 24, "编辑 · 车马练习", ink)
	txt(p, 282, 107, 13, "编辑研究起点 · 修改摆子将保存编辑前副本", muted)
	button(p, 869, 70, 90, 34, "原图", false)
	board(p, 380, 153, 554, 612, false)
	txt(p, 384, 788, 13, "选择托盘棋子，再点棋盘空落点", muted)
	button(p, 383, 825, 103, 42, "撤销", false)
	button(p, 497, 825, 103, 42, "重做", false)
	button(p, 712, 825, 107, 42, "保存", false)
	button(p, 830, 825, 106, 42, "开始推演", true)
	txt(p, 1104, 71, 19, "摆棋工具", ink)
	txt(p, 1104, 122, 13, "红方库存", muted)
	labels := []string{"帅", "仕", "相", "马", "车", "炮", "兵"}
	counts := []string{"0", "2", "2", "1", "1", "2", "5"}
	for i, s := range labels {
		x := float32(1104 + (i%4)*76)
		y := float32(155 + (i/4)*82)
		box(p, x, y, 64, 64, card)
		center(p, x, y, 64, 42, 25, s, red, "STKaiti")
		center(p, x, y+42, 64, 18, 11, "剩余 "+counts[i], muted, "")
	}
	txt(p, 1104, 332, 13, "黑方库存", muted)
	for i, s := range []string{"将", "士", "象", "马", "车", "炮", "卒"} {
		x := float32(1104 + (i%4)*76)
		y := float32(365 + (i/4)*82)
		box(p, x, y, 64, 64, card)
		center(p, x, y, 64, 42, 25, s, ink, "STKaiti")
		center(p, x, y+42, 64, 18, 11, "剩余 "+[]string{"0", "2", "2", "2", "2", "2", "4"}[i], muted, "")
	}
	p.Line(1104, 549, 1408, 549, 1, line)
	txt(p, 1104, 578, 14, "先行方", ink)
	button(p, 1206, 568, 94, 34, "红先", true)
	button(p, 1309, 568, 94, 34, "黑先", false)
	txt(p, 1104, 633, 14, "棋盘朝向", ink)
	button(p, 1206, 623, 197, 34, "红方在下", false)
	button(p, 1104, 690, 143, 38, "初始盘", false)
	button(p, 1260, 690, 143, 38, "清空棋盘", false)
	button(p, 1104, 759, 299, 42, "从图片导入", false)
	txt(p, 1104, 826, 12, "未完成局面可以保存为草稿", muted)
}
func view(mode string) func(*ui.Context) {
	return func(c *ui.Context) {
		c.Root().Background(paper)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: 0, Y: 0, W: 1440, H: 44}, card, 0)
			center(p, 0, 0, 1440, 44, 13, "象棋残局  ·  桌面设计提案", muted, "")
			p.Fill(ui.Rect{X: 0, Y: 44, W: 252, H: 872}, card.Alpha(0.5), 0)
			p.Fill(ui.Rect{X: 1080, Y: 44, W: 360, H: 872}, card.Alpha(0.45), 0)
			p.Line(252, 44, 252, 916, 1, line)
			p.Line(1080, 44, 1080, 916, 1, line)
			library(p)
			if mode == "study" {
				study(p)
			} else {
				editor(p)
			}
			p.Line(0, 916, 1440, 916, 1, line)
			txt(p, 24, 931, 12, "离线引擎  ·  本地残局", muted)
			txt(p, 618, 931, 11, "设计稿 · 固定示意数据 · 非完整应用", muted)
			txt(p, 1216, 931, 12, "mygo Native UI 离屏绘制", muted)
		})
	}
}
func main() {
	if len(os.Args) != 2 {
		panic("output directory required")
	}
	for _, mode := range []string{"study", "editor"} {
		im := ui.Render(view(mode), 1440, 960, 1)
		path := os.Args[1] + "/desktop-" + mode + ".png"
		f, err := os.Create(path)
		if err != nil {
			panic(err)
		}
		if err = png.Encode(f, im); err != nil {
			panic(err)
		}
		f.Close()
		fmt.Println(path)
	}
}
