package ui

import (
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func TestUpdateSettingsActions(t *testing.T) {
	checks, downloads, requested := true, false, 0
	a := New(t.TempDir(), "")
	a.updates = &updateControls{
		check:  func() { requested++ },
		checks: func() bool { return checks }, setChecks: func(on bool) { checks = on },
		downloads: func() bool { return downloads }, setDownloads: func(on bool) { downloads = on },
		lastCheck: func() time.Time { return time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC) },
	}
	tt := native.NewTester(a.updateSettings, 800, 600)
	click(t, tt, "自动检查更新")
	if checks {
		t.Fatal("automatic checks were not disabled")
	}
	click(t, tt, "自动下载并安装")
	if downloads {
		t.Fatal("automatic downloads could be enabled while checks are disabled")
	}
	click(t, tt, "检查更新")
	if requested != 1 {
		t.Fatal("manual check did not reach the updater")
	}
	click(t, tt, "自动检查更新")
	click(t, tt, "自动下载并安装")
	if !checks || !downloads {
		t.Fatal("update preferences were lost during UI rebuilds")
	}
	click(t, tt, "自动下载并安装")
	if downloads {
		t.Fatal("automatic downloads were not disabled")
	}
}
