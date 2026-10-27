package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/diagnostics"
	native "github.com/egoist/mygo/ui"
)

type diagnosticFixture struct {
	previous []diagnostics.Report
	err      error
	ack      chan struct{}
}

func (f *diagnosticFixture) LogDirectory() string           { return "unused" }
func (f *diagnosticFixture) Previous() []diagnostics.Report { return f.previous }
func (f *diagnosticFixture) Acknowledge() error             { f.ack <- struct{}{}; return nil }
func (f *diagnosticFixture) Preview() (string, error)       { return "fixture preview", nil }
func (f *diagnosticFixture) Export(string) error            { return nil }
func (f *diagnosticFixture) Err() error                     { return f.err }

func TestDiagnosticNoticeDistinguishesUnknownExit(t *testing.T) {
	for _, crashed := range []bool{false, true} {
		a := New(t.TempDir(), "")
		fixture := &diagnosticFixture{previous: []diagnostics.Report{{ID: "previous", Unclean: true, HasCrash: crashed}}, ack: make(chan struct{}, 1)}
		a.setDiagnosticSource(fixture, nil)
		if strings.Contains(a.diagnostic.notice, "崩溃报告") != crashed {
			t.Fatalf("incorrect notice: %s", a.diagnostic.notice)
		}
		tt := native.NewTester(a.diagnosticDialogs, 1000, 800)
		click(t, tt, "稍后")
		if a.diagnostic.noticeOpen {
			t.Fatal("notice stayed open")
		}
		select {
		case <-fixture.ack:
		case <-time.After(time.Second):
			t.Fatal("notice was not acknowledged")
		}
		tt.Frame()
		if a.diagnostic.noticeOpen {
			t.Fatal("dismissed notice repeated")
		}
	}
}

func TestDiagnosticPreviewAndWriteWarning(t *testing.T) {
	a := New(t.TempDir(), "")
	fixture := &diagnosticFixture{err: errors.New("fixture disk failure"), ack: make(chan struct{}, 1)}
	a.setDiagnosticSource(fixture, nil)
	tt := native.NewTester(a.diagnosticDialogs, 1000, 800)
	click(t, tt, "知道了")
	tt.Frame()
	if a.diagnostic.warningOpen {
		t.Fatal("disk error warning repeated")
	}
	a.diagnostic.open, a.diagnostic.preparing = true, true
	a.diagnostic.results <- diagnosticResult{preview: "retained crash fixture"}
	tt.Frame()
	if a.diagnostic.preparing || a.diagnostic.preview != "retained crash fixture" {
		t.Fatal("preview result was not applied on the UI thread")
	}
	click(t, tt, "关闭预览")
	if a.diagnostic.open {
		t.Fatal("preview did not close")
	}
}
