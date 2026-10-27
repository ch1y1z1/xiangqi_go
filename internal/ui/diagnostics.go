package ui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/diagnostics"
	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

type diagnosticSource interface {
	LogDirectory() string
	Previous() []diagnostics.Report
	Acknowledge() error
	Preview() (string, error)
	Export(string) error
	Err() error
}

type diagnosticState struct {
	source                          diagnosticSource
	open, preparing, exporting      bool
	noticeOpen, warningOpen, warned bool
	notice, preview, error          string
	results                         chan diagnosticResult
}

type diagnosticResult struct {
	preview  string
	exported bool
	err      error
}

func (a *App) SetDiagnostics(source *diagnostics.Session, err error) {
	if source == nil {
		a.setDiagnosticSource(nil, err)
		return
	}
	a.setDiagnosticSource(source, err)
}

func (a *App) setDiagnosticSource(source diagnosticSource, err error) {
	d := &a.diagnostic
	d.results = make(chan diagnosticResult, 2)
	d.source = source
	d.warningOpen, d.warned = err != nil, err != nil
	if source != nil {
		previous := source.Previous()
		if len(previous) > 0 {
			d.noticeOpen = true
			d.notice = "上次运行未正常结束，已保留诊断日志。强制结束或系统关机也可能造成此情况。"
			for _, report := range previous {
				if report.HasCrash {
					d.notice = "检测到上次运行的崩溃报告，日志已保留，可预览后导出用于排查。"
					break
				}
			}
		}
	}
}

func (a *App) diagnosticSettings(c *native.Context) {
	d := &a.diagnostic
	section(c, "诊断日志")
	if d.source == nil {
		native.Text(c, "本次运行未启用文件日志。").FontSize(12).TextColor(muted)
	} else {
		native.Text(c, "错误和崩溃记录保存在本机，可预览后导出用于排查。").FontSize(12).TextColor(muted)
	}
	native.Row(c).Gap(10).Children(func() {
		action(c, "打开日志文件夹", false, d.source == nil, a.openLogDirectory)
		action(c, "预览与导出诊断包", false, d.source == nil || d.exporting, a.openDiagnostics)
	})
	rule(c)
}

func (a *App) openLogDirectory() {
	if source := a.diagnostic.source; source != nil {
		if err := mygo.Shell.OpenPath(source.LogDirectory()); err != nil {
			diagnostics.Error("open_log_directory", err)
			a.errorMessage = "无法打开日志文件夹：" + err.Error()
		}
	}
}

func (a *App) openDiagnostics() {
	d := &a.diagnostic
	if d.source == nil || d.preparing || d.exporting {
		return
	}
	d.open, d.preparing, d.error = true, true, ""
	d.preview = "正在读取诊断日志…"
	if d.results == nil {
		d.results = make(chan diagnosticResult, 2)
	}
	source, results := d.source, d.results
	go func() {
		preview, err := source.Preview()
		results <- diagnosticResult{preview: preview, err: err}
		a.update(func() {})
	}()
}

func (a *App) exportDiagnostics() {
	d := &a.diagnostic
	if d.source == nil || d.preparing || d.exporting {
		return
	}
	d.exporting, d.error = true, ""
	source, results := d.source, d.results
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: a.win, Title: "导出诊断包", DefaultPath: "xiangqi-diagnostics-" + time.Now().Format("20060102-150405") + ".zip",
			Filters: []mygo.FileFilter{{Name: "诊断包", Extensions: []string{"zip"}}},
		})
		exported := false
		if err == nil && path != "" {
			if !strings.EqualFold(filepath.Ext(path), ".zip") {
				path += ".zip"
			}
			err = source.Export(path)
			exported = err == nil
		}
		results <- diagnosticResult{exported: exported, err: err}
		a.update(func() {})
	}()
}

func (a *App) diagnosticDialogs(c *native.Context) {
	d := &a.diagnostic
	for d.results != nil {
		select {
		case result := <-d.results:
			if d.exporting {
				d.exporting = false
				if result.exported {
					a.status = "诊断包已导出"
				}
			} else {
				d.preparing, d.preview = false, result.preview
			}
			if result.err != nil {
				diagnostics.Error("diagnostic_report", result.err)
				d.error = "无法完成诊断操作：" + result.err.Error()
			}
		default:
			goto drained
		}
	}
drained:
	if d.source != nil && !d.warned && d.source.Err() != nil {
		d.warningOpen, d.warned = true, true
	}
	if d.warningOpen {
		native.AlertDialog(c, &d.warningOpen, "诊断日志提示", "部分诊断信息无法记录或读取，本次运行仍可继续使用。", "知道了")
	}
	if d.noticeOpen {
		which := native.AlertDialog(c, &d.noticeOpen, "上次运行的诊断记录", d.notice, "稍后", "查看诊断")
		if !d.noticeOpen {
			source := d.source
			go func() { diagnostics.Error("acknowledge_diagnostic_notice", source.Acknowledge()) }()
			if which == 1 {
				a.openDiagnostics()
			}
		}
	}
	native.DialogBase(c, &d.open, func(back, panel native.Element) {
		w, h := c.Size()
		back.Background(ink.Alpha(.25))
		panel.Width(min(float32(820), w-48)).MaxHeight(h - 64).Padding(24).Gap(12).Radius(16).Background(paper)
		native.Text(c, "诊断日志预览").FontSize(22).FontWeight(600)
		native.Text(c, "诊断包只包含日志和版本信息。原始崩溃报告可能含本地路径或异常文本，请确认后导出。").FontSize(12).TextColor(muted)
		native.Scroll(c).FillWidth().MaxHeight(max(float32(120), h-340)).Children(func() {
			native.Text(c, d.preview).FontSize(12)
		})
		if d.error != "" {
			native.Text(c, d.error).TextColor(red).FontSize(12)
		}
		native.Row(c).Gap(10).Justify(native.End).Children(func() {
			action(c, "关闭预览", false, d.exporting, func() { d.open = false })
			action(c, "打开日志文件夹", false, false, a.openLogDirectory)
			label := "导出诊断包"
			if d.exporting {
				label = "正在导出…"
			}
			action(c, label, true, d.preparing || d.exporting || d.error != "", a.exportDiagnostics)
		})
	})
}
