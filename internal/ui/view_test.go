package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/recognition"
	"github.com/ch1y1z1/xiangqi_go/internal/session"
	"github.com/ch1y1z1/xiangqi_go/internal/store"
	native "github.com/egoist/mygo/ui"
)

func TestNativeResearchControlAndUndo(t *testing.T) {
	study := domain.NewStudy("托管回退测试", domain.InitialPieces(), domain.Red)
	study.IsDraft = false
	for _, uci := range []string{"a3a4", "a6a5"} {
		m, err := domain.ParseMove(uci)
		if err != nil {
			t.Fatal(err)
		}
		study.Play(m)
	}
	a := New(t.TempDir(), "")
	a.loading, a.showLibrary = false, false
	s := session.New(study, nil, nil)
	s.SetAI(domain.Black)
	a.research = s
	tt := native.NewTester(a.View, 1100, 850)
	tt.SetPreferences(native.Preferences{ReduceMotion: true})
	click(t, tt, "暂停托管") // Available even while waiting for the human.
	click(t, tt, "回退")
	if !s.Paused || s.Study.CurrentID != study.RootID || len(s.Study.Nodes) != 3 {
		t.Fatal("footer undo did not preserve control and the saved line")
	}
	tt.Key(native.Alt, native.KeyRight)
	if len(s.Study.History()) != 1 || !s.Paused {
		t.Fatal("forward shortcut changed pause state")
	}
	tt.Key(native.Alt, native.KeyLeft)
	if s.Study.CurrentID != study.RootID || !s.Paused {
		t.Fatal("undo shortcut differed from the footer")
	}
	click(t, tt, "继续托管")
	if s.Paused || s.AISide != domain.Black {
		t.Fatal("resume changed the assigned AI side")
	}
	for _, size := range [][2]int{{1440, 900}, {960, 680}} {
		tt.SetSize(size[0], size[1])
		for _, label := range []string{"起点", "回退", "前进", "AI 执黑", "暂停托管"} {
			r, ok := tt.Find(label)
			if !ok || r.X < 0 || r.X+r.W > float32(size[0]) || r.Y < 0 || r.Y+r.H > float32(size[1]) {
				t.Fatalf("control %q is outside the %dx%d window: %+v", label, size[0], size[1], r)
			}
		}
		if directory := os.Getenv("XIANGQI_UI_PROBES"); directory != "" {
			if err := os.MkdirAll(directory, 0755); err != nil {
				t.Fatal(err)
			}
			name := "research-wide.png"
			if size[0] == 960 {
				name = "research-narrow.png"
			}
			f, err := os.Create(filepath.Join(directory, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("save preview: %v %v", err, closeErr)
			}
		}
	}
	click(t, tt, "AI 执黑")
	if err := tt.ChooseMenuItem("我执黑 / AI 执红"); err != nil {
		t.Fatal(err)
	}
	tt.Key(native.Alt, native.KeyRight) // Only the AI opening is on the line.
	if len(s.Study.History()) != 1 || s.CanBack() {
		t.Fatal("the AI opening should not be undoable as a human move")
	}
	current := s.Study.CurrentID
	click(t, tt, "回退")
	tt.Key(native.Alt, native.KeyLeft)
	if s.Study.CurrentID != current {
		t.Fatal("disabled undo button or shortcut removed the AI opening")
	}
	click(t, tt, "AI 执红")
	if err := tt.ChooseMenuItem("双方手动"); err != nil {
		t.Fatal(err)
	}
	click(t, tt, "回退")
	if s.Study.CurrentID != study.RootID || s.AISide != "" {
		t.Fatal("manual mode did not undo one move")
	}
}

func click(t *testing.T, tt *native.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func TestNativeWorkspaceRebuilds(t *testing.T) {
	a := New(t.TempDir(), "")
	a.loading = false
	a.beginEdit(domain.NewStudy("迁移测试", nil, domain.Red))
	tt := native.NewTester(a.View, 1440, 900)
	tt.SetPreferences(native.Preferences{ReduceMotion: true})
	click(t, tt, "收起残局库")
	if a.showLibrary {
		t.Fatal("sidebar did not close")
	}
	click(t, tt, "打开残局库")
	if !a.showLibrary {
		t.Fatal("sidebar did not reopen")
	}
	click(t, tt, "分析 / 工具")
	if a.showTools {
		t.Fatal("tools did not close")
	}
	click(t, tt, "分析 / 工具")
	if !a.showTools {
		t.Fatal("tools did not reopen")
	}
	tt.SetSize(1100, 900)
	click(t, tt, "打开残局库")
	if !a.libraryDrawer {
		t.Fatal("narrow workspace did not open library drawer")
	}
	a.libraryDrawer = false
	tt.Frame()
	tt.SetSize(1440, 900)
	click(t, tt, "残局名称")
	tt.Key(native.Cmd, native.KeyA)
	tt.Type("新版残局")
	if a.editor.name != "新版残局" || !a.editor.dirty {
		t.Fatalf("name edit was not applied: %+v", a.editor)
	}
	a.editor.saving = true
	tt.Frame()
	tt.Type("不应写入")
	if a.editor.name != "新版残局" {
		t.Fatal("disabled name input accepted an edit")
	}
}

func TestNativeSettingsProviderKeys(t *testing.T) {
	a := New(t.TempDir(), "")
	a.loading = false
	a.openSettings()
	tt := native.NewTester(a.settingsDialog, 1000, 1100)
	clickKey := func() {
		// The section title and input share a label; click the field below it.
		r, _ := tt.Find("API 密钥")
		tt.ClickAt(r.X+r.W/2, r.Y+r.H+30)
		if !tt.Focused("API 密钥") {
			t.Fatal("API key input was not focused")
		}
	}
	clickKey()
	tt.Type("deepseek-test")
	if a.settings.keys[recognition.DeepSeek] != "deepseek-test" {
		t.Fatalf("API key edit was not applied: %v", a.settings.keys)
	}
	click(t, tt, "自定义服务")
	if a.settings.draft.Recognition.Provider != recognition.Custom {
		t.Fatal("provider radio did not update")
	}
	clickKey()
	tt.Type("custom-test")
	click(t, tt, "DeepSeek 官方")
	if a.settings.keys[recognition.DeepSeek] != "deepseek-test" || a.settings.keys[recognition.Custom] != "custom-test" {
		t.Fatalf("provider keys crossed or lost edits: %v", a.settings.keys)
	}
	clickKey()
	tt.Key(native.Cmd, native.KeyA)
	tt.Type("deepseek-updated")
	if a.settings.keys[recognition.DeepSeek] != "deepseek-updated" {
		t.Fatal("returning to a keyed input lost the edit")
	}
	a.settings.saving = true
	tt.Frame()
	tt.Type("不应写入")
	click(t, tt, "移除此服务的密钥")
	if a.settings.keys[recognition.DeepSeek] != "deepseek-updated" || a.settings.remove[recognition.DeepSeek] {
		t.Fatal("disabled settings accepted an edit or action")
	}
}

func TestNativeRenameSubmitAndSave(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(t.TempDir(), "")
	a.store, a.studies = s, s.List()
	id := a.studies[0].ID
	for _, submit := range []bool{true, false} {
		a.rename(a.studies[0])
		tt := native.NewTester(a.dialogs, 900, 700)
		tt.Key(native.Cmd, native.KeyA)
		name := "回车提交"
		if !submit {
			name = "按钮保存"
		}
		tt.Type(name)
		if submit {
			tt.Key(0, native.KeyEnter)
		} else {
			click(t, tt, "保存名称")
		}
		if a.showRename || a.errorMessage != "" {
			t.Fatalf("rename did not finish: open=%v error=%s", a.showRename, a.errorMessage)
		}
		for _, study := range s.List() {
			if study.ID == id && study.Name != name {
				t.Fatalf("persisted stale name: got %q want %q", study.Name, name)
			}
		}
	}
}
