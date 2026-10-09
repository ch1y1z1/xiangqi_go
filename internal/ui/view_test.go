package ui

import (
	"testing"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/recognition"
	"github.com/ch1y1z1/xiangqi_go/internal/store"
	native "github.com/egoist/mygo/ui"
)

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
