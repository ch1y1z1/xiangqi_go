package ui

import (
	"log/slog"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	native "github.com/egoist/mygo/ui"
)

// The plugin owns scheduling, persistent preferences, download verification and
// installation. Keeping its actions here also lets UI tests run without a live
// plugin, network requests, or changes to the user's updater.json.
type updateControls struct {
	check        func()
	checks       func() bool
	setChecks    func(bool)
	downloads    func() bool
	setDownloads func(bool)
	lastCheck    func() time.Time
}

func (a *App) attachUpdater() {
	a.updates = &updateControls{
		check: updater.CheckForUpdates, checks: updater.AutomaticChecks,
		setChecks: updater.SetAutomaticChecks, downloads: updater.AutomaticDownloads,
		setDownloads: updater.SetAutomaticDownloads, lastCheck: updater.LastCheck,
	}
	a.detachUpdates = updater.OnChange(func() {
		if !a.closed {
			a.win.Invalidate()
		}
	})
}

func (a *App) checkUpdates() {
	if a.updates != nil {
		slog.Info("update_check_requested")
		a.updates.check()
	}
}

func (a *App) updateSettings(c *native.Context) {
	u := a.updates
	if u == nil {
		return
	}
	section(c, "应用更新")
	checks := u.checks()
	if native.Checkbox(c, &checks, "自动检查更新").Changed() {
		u.setChecks(checks)
	}
	downloads := u.downloads()
	if native.Checkbox(c, &downloads, "自动下载并安装").Disabled(!checks).Changed() {
		u.setDownloads(downloads)
	}
	native.Text(c, "每天检查新版本；自动安装后下次启动生效。更新选项立即生效。").FontSize(12).TextColor(muted)
	if checked := u.lastCheck(); !checked.IsZero() {
		native.Text(c, "上次检查："+checked.Local().Format("2006-01-02 15:04")).FontSize(12).TextColor(muted)
	}
	action(c, "检查更新", false, false, a.checkUpdates)
	rule(c)
}

func applicationVersion() string {
	if version := mygo.App.Version(); version != "" {
		return version
	}
	return "开发版"
}
