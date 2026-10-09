package ui

import (
	"strings"

	native "github.com/egoist/mygo/ui"
)

func (a *App) titleInfo() (title, state string) {
	title = "象棋残局"
	if a.editor != nil {
		title = strings.TrimSpace(a.editor.name)
		state = "编辑中"
		if a.editor.dirty {
			state = "未保存"
		}
	} else if a.research != nil {
		title = a.research.Study.Name
	}
	if title == "" {
		title = "未命名残局"
	}
	return
}

func (a *App) syncWindowTitle() {
	title, _ := a.titleInfo()
	if a.editor != nil {
		title = "编辑 · " + title
	}
	if a.win != nil && a.windowTitle != title {
		a.windowTitle = title
		a.win.SetTitle(title)
	}
}

// Each pane paints its own header and body. The sidebar background reaches
// behind the native traffic lights, and the split continues to the top edge.
func (a *App) sidebarTitleBar(c *native.Context) {
	bar := c.TitleBar()
	native.Row(c).FillWidth().Height(max(bar.Height, 56)).Shrink(0).
		Padding(0, 12, 0, bar.Left+12).AlignItems(native.Center).DragWindow().Children(func() {
		native.Spacer(c)
		headerIcon(c, iconSidebar, "收起残局库", false, func() { a.showLibrary = false })
	})
}

func (a *App) workspaceTitleBar(c *native.Context, width float32, hasSidebar bool) {
	bar := c.TitleBar()
	left := float32(18)
	if !hasSidebar {
		left += bar.Left
	}
	title, state := a.titleInfo()
	native.Row(c).FillWidth().Height(max(bar.Height, 56)).Shrink(0).
		Padding(0, bar.Right+16, 0, left).Gap(10).AlignItems(native.Center).DragWindow().Children(func() {
		if !hasSidebar {
			headerIcon(c, iconSidebar, "打开残局库", a.loading, func() {
				if width < 1280 {
					a.libraryDrawer = true
				} else {
					a.showLibrary = true
				}
			})
		}
		native.Icon(c, iconBoard).Size(17, 17).TextColor(muted).Shrink(0)
		native.Text(c, title).Grow(1).MinWidth(0).FontSize(14).FontWeight(500).SingleLine().Tooltip(title)
		if state != "" {
			native.Text(c, state).Shrink(0).FontSize(12).TextColor(muted)
		}
		native.Toolbar(c, func() {
			headerIcon(c, iconNew, "新建残局", a.loading, a.newStudy).Tooltip("新建残局 · ⌘N")
			headerIcon(c, iconSettings, "设置", a.loading, a.openSettings).Tooltip("设置 · ⌘,")
			headerIcon(c, iconTools, "分析 / 工具", false, func() { a.showTools = !a.showTools }).Tooltip("显示或隐藏研究面板")
		}).Label("窗口工具栏").Width(102).Padding(0).Gap(6).Shrink(0)
	})
}

func headerIcon(c *native.Context, icon *native.SVG, label string, disabled bool, fn func()) native.Element {
	e := native.ButtonBase(c).Size(30, 30).Shrink(0).Radius(6).TextColor(muted).Label(label).Tooltip(label).Disabled(disabled)
	if e.Pressed() {
		e.Background(ink.Alpha(0.10))
	} else if e.Hovered() {
		e.Background(ink.Alpha(0.06))
	}
	e.Children(func() { native.Icon(c, icon).Size(18, 18) })
	if fn != nil {
		e.OnClick(fn)
	}
	return e
}

func sidebarCommand(c *native.Context, icon *native.SVG, label string, fn func()) {
	e := native.ButtonBase(c).FillWidth().MinHeight(36).Padding(8, 10).Gap(10).Radius(8).Label(label)
	if e.Pressed() {
		e.Background(sidebarSelected)
	} else if e.Hovered() {
		e.Background(sidebarHover)
	}
	e.Children(func() {
		native.Icon(c, icon).Size(18, 18).TextColor(muted).Shrink(0)
		native.Text(c, label).Grow(1).MinWidth(0).FontSize(14).SingleLine()
	})
	if fn != nil {
		e.OnClick(fn)
	}
}
