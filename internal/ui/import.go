package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/credentials"
	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/recognition"
	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

type importState struct {
	open, processing, starting bool
	image                      []byte
	bitmap                     *native.Bitmap
	name, error, jobID         string
	generation                 int
	zoom                       float32
	panX, panY                 float32
}

func (a *App) chooseImage() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "选择棋盘图片", Filters: []mygo.FileFilter{{Name: "棋盘图片", Extensions: []string{"png", "jpg", "jpeg", "heic", "heif", "webp", "gif", "bmp", "tif", "tiff"}}}})
		a.update(func() {
			if err != nil {
				a.imageImport.error = "无法选择图片：" + err.Error()
			} else if len(paths) > 0 {
				a.loadImage(paths[0])
			}
		})
	}()
}

func (a *App) loadImage(path string) {
	s := &a.imageImport
	if a.jobs != nil {
		if r := a.jobs.Record(); r != nil && r.Status == recognition.Running {
			s.error = "已有图片正在识别，请先停止或等待完成。"
			s.open = true
			return
		}
	}
	s.open = true
	s.processing = true
	s.error = ""
	s.generation++
	generation := s.generation
	go func() {
		data, err := recognition.PrepareFile(path)
		var bitmap *native.Bitmap
		if err == nil {
			bitmap, err = native.DecodeBitmap(data)
		}
		a.update(func() {
			if generation != s.generation {
				return
			}
			s.processing = false
			if err != nil {
				s.error = err.Error()
				return
			}
			s.image = data
			s.bitmap = bitmap
			s.name = filepath.Base(path)
			s.zoom = 1
			s.panX = 0
			s.panY = 0
			s.jobID = "selected"
		})
	}()
}

func (a *App) startRecognition() {
	s := &a.imageImport
	if s.starting || s.processing || len(s.image) == 0 || a.jobs == nil {
		return
	}
	if r := a.jobs.Record(); r != nil && r.Status == recognition.Ready {
		a.confirm("替换尚未导入的识别结果？", "当前结果会被清理，新识别使用所选图片。", "开始新识别", a.performRecognition)
		return
	}
	a.performRecognition()
}
func (a *App) performRecognition() {
	s := &a.imageImport
	s.starting = true
	s.error = ""
	data := append([]byte(nil), s.image...)
	settings := a.prefs.Recognition
	go func() {
		key, err := credentials.Get(keyService(settings.Provider))
		if err == nil {
			err = a.jobs.Start(data, key, settings)
		}
		a.update(func() {
			s.starting = false
			if err != nil {
				s.error = err.Error()
			}
		})
	}()
}

func (a *App) importResult() {
	if a.jobs == nil {
		return
	}
	record := a.jobs.Record()
	if record == nil || record.Status != recognition.Ready || record.Result == nil {
		return
	}
	setup := *record.Result
	if err := setup.Validate(); err != nil {
		a.imageImport.error = err.Error()
		return
	}
	bitmap := a.imageImport.bitmap
	if a.imageImport.jobID != record.ID || bitmap == nil {
		var err error
		bitmap, err = native.DecodeBitmap(a.jobs.Image())
		if err != nil {
			a.imageImport.error = "无法打开本次识别的原图：" + err.Error()
			return
		}
	}
	if a.editor == nil {
		a.guardEdit(func() { a.beginEdit(domain.NewStudy("新残局", nil, domain.Red)); a.applyRecognition(setup, bitmap) })
		return
	}
	a.applyRecognition(setup, bitmap)
}
func (a *App) applyRecognition(setup recognition.Setup, bitmap *native.Bitmap) {
	ed := a.editor
	if ed == nil || ed.saving {
		return
	}
	ed.board.Replace(setup.ChessPieces())
	ed.side = domain.Red
	if setup.SideToMove != "" {
		ed.side = domain.Side(setup.SideToMove)
	}
	ed.bottom = domain.Red
	ed.image = bitmap
	ed.notes = strings.TrimSpace(setup.Notes)
	ed.dirty = true
	if ed.name == "新残局" && strings.TrimSpace(setup.Name) != "" {
		ed.name = strings.TrimSpace(setup.Name)
	}
	ed.message = "请对照原图校正棋子与先行方。"
	if setup.SideToMove == "" {
		ed.message = "请对照原图校正；先行方暂选红先。"
	}
	if err := a.jobs.Clear(); err != nil {
		a.errorMessage = err.Error()
	}
	a.imageImport = importState{}
	a.sourceZoom = 1
	a.sourceX = 0
	a.sourceY = 0
}

func (a *App) importDialog(c *native.Context) {
	s := &a.imageImport
	if s.open && s.bitmap == nil && a.jobs != nil {
		if r := a.jobs.Record(); r != nil && s.jobID != r.ID {
			s.jobID = r.ID
			s.image = a.jobs.Image()
			s.bitmap, _ = native.DecodeBitmap(s.image)
			s.zoom = 1
		}
	}
	native.DialogBase(c, &s.open, func(back, panel native.Element) {
		w, h := c.Size()
		back.Background(ink.Alpha(.25))
		panel.Width(min(float32(820), w-48)).Height(min(float32(760), h-64)).Padding(22).Gap(12).Radius(16).Background(paper)
		native.Row(c).AlignItems(native.Center).Children(func() {
			native.Text(c, "从图片识别残局").FontSize(22).FontWeight(600).Grow(1)
			action(c, "服务设置", false, s.starting, a.openSettings)
			action(c, "返回", false, s.starting, func() { s.open = false })
		})
		if s.bitmap != nil {
			imageViewer(c, s.bitmap, &s.zoom, &s.panX, &s.panY).Grow(1).FillWidth()
		} else {
			zone := native.Column(c).Grow(1).FillWidth().Center().Gap(14).Radius(12).Border(1, line)
			zone.Children(func() {
				native.Text(c, "拖入一张棋盘图片").FontSize(23).TextColor(teal)
				native.Text(c, "支持 JPEG、PNG、HEIC 等图片").TextColor(muted)
				action(c, "选择图片", true, s.processing, a.chooseImage)
			})
			if files := zone.DroppedFiles(); len(files) > 0 {
				a.loadImage(files[0])
			}
		}
		var record *recognition.JobRecord
		if a.jobs != nil {
			record = a.jobs.Record()
		}
		if s.error != "" {
			native.Text(c, s.error).TextColor(red).FontSize(13)
		}
		if s.processing {
			native.Row(c).Gap(8).Children(func() { native.Spinner(c); native.Text(c, "正在处理图片…") })
		} else if record != nil && record.Status == recognition.Running {
			native.Row(c).Gap(8).Children(func() {
				native.Spinner(c)
				native.Textf(c, "正在识别 · 已等待 %.0f 秒", time.Since(record.StartedAt).Seconds())
			})
			c.After(time.Second)
		} else if record != nil && record.Status == recognition.Ready {
			if s.jobID == "selected" {
				native.Text(c, "上次结果尚未导入。导入会使用上次原图；识别新图片会替换上次结果。").TextColor(teal).FontSize(13)
			} else {
				native.Textf(c, "已识别 %d 枚棋子 · 导入后对照原图校正", len(record.Result.Pieces)).TextColor(teal)
			}
		} else if record != nil && record.Status == recognition.Failed {
			native.Text(c, record.Error).TextColor(red).FontSize(13)
		} else {
			native.Text(c, "预览后点击识别才发送图片；摆棋、保存与 AI 研究均离线。").TextColor(muted).FontSize(12)
		}
		native.Row(c).Gap(10).AlignItems(native.Center).Children(func() {
			running := record != nil && record.Status == recognition.Running
			action(c, "换一张图片", false, running || s.processing || s.starting, a.chooseImage)
			native.Box(c).Grow(1)
			if running {
				action(c, "停止识别", true, false, func() {
					if err := a.jobs.Stop(); err != nil {
						s.error = err.Error()
					}
				})
			} else if record != nil && record.Status == recognition.Ready {
				action(c, "丢弃结果", false, false, func() { _ = a.jobs.Clear() })
				if s.jobID == "selected" {
					action(c, "识别新图片", false, s.processing || s.starting, a.startRecognition)
				}
				action(c, "导入并校正", true, false, a.importResult)
			} else {
				label := "识别棋盘"
				if s.starting {
					label = "准备识别…"
				}
				action(c, label, true, len(s.image) == 0 || s.processing || s.starting || a.jobs == nil, a.startRecognition)
			}
		})
	})
}

func (a *App) sourceDialog(c *native.Context) {
	native.DialogBase(c, &a.sourceOpen, func(back, panel native.Element) {
		w, h := c.Size()
		back.Background(ink.Alpha(.25))
		panel.Width(w - 64).Height(h - 64).Padding(18).Gap(10).Radius(16).Background(paper)
		native.Row(c).AlignItems(native.Center).Children(func() {
			native.Text(c, "原图 · 对照校正").FontSize(21).FontWeight(600).Grow(1)
			action(c, "关闭", false, false, func() { a.sourceOpen = false })
		})
		if a.editor != nil && a.editor.image != nil {
			imageViewer(c, a.editor.image, &a.sourceZoom, &a.sourceX, &a.sourceY).Grow(1).FillWidth()
			if a.editor.notes != "" {
				native.Text(c, a.editor.notes).TextColor(muted).FontSize(12)
			}
		}
	})
}

func imageViewer(c *native.Context, b *native.Bitmap, zoom, panX, panY *float32) native.Element {
	e := native.Column(c).Gap(8).MinHeight(0)
	e.Children(func() {
		zone := native.Box(c).Grow(1).FillWidth().MinHeight(0).Clip().Background(native.Hex("#EDE7DC")).Radius(12).Label("图片预览：滚轮缩放，拖动平移，双击适配")
		zone.HandleInput(func(ev native.InputEvent) bool {
			switch ev.Kind {
			case native.InputScroll:
				*zoom = max(float32(1), min(float32(8), *zoom*float32(math.Exp(float64(-ev.DY)*.008))))
				if *zoom == 1 {
					*panX = 0
					*panY = 0
				}
				return true
			}
			return false
		})
		dx, dy, _ := zone.Dragged()
		if *zoom > 1 {
			*panX += dx
			*panY += dy
		}
		if zone.DoubleClicked() {
			*zoom = 1
			*panX = 0
			*panY = 0
		}
		zone.Draw(func(p *native.Painter, r native.Rect) {
			bw, bh := b.Size()
			scale := min(r.W/float32(bw), r.H/float32(bh))
			iw, ih := float32(bw)*scale**zoom, float32(bh)*scale**zoom
			p.Image(b, native.Rect{X: r.X + (r.W-iw)/2 + *panX, Y: r.Y + (r.H-ih)/2 + *panY, W: iw, H: ih}, native.Contain)
		})
		native.Row(c).Gap(8).AlignItems(native.Center).Children(func() {
			action(c, "−", false, *zoom <= 1, func() { *zoom = max(float32(1), *zoom/1.25) })
			native.Text(c, fmt.Sprintf("%.0f%%", *zoom*100)).FontSize(12).TextColor(muted)
			action(c, "＋", false, *zoom >= 8, func() { *zoom = min(float32(8), *zoom*1.25) })
			action(c, "适配", false, false, func() { *zoom = 1; *panX = 0; *panY = 0 })
		})
	})
	return e
}
