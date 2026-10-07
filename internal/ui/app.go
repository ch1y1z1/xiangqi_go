package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
	"github.com/ch1y1z1/xiangqi_go/internal/preferences"
	"github.com/ch1y1z1/xiangqi_go/internal/recognition"
	"github.com/ch1y1z1/xiangqi_go/internal/session"
	"github.com/ch1y1z1/xiangqi_go/internal/store"
	"github.com/ch1y1z1/xiangqi_go/internal/ui/chessboard"
	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

type editorState struct {
	original      domain.Study
	board         *domain.Editor
	name          string
	side, bottom  domain.Side
	dirty, saving bool
	message       string
	image         *native.Bitmap
	notes         string
}

type App struct {
	win                                         *mygo.Window
	windowTitle                                 string
	dataDir, resourcesDir                       string
	prefs                                       preferences.Settings
	store                                       *store.Store
	engine                                      *engine.Client
	jobs                                        *recognition.Jobs
	studies                                     []domain.Study
	research                                    *session.Session
	editor                                      *editorState
	board                                       chessboard.State
	loading                                     bool
	engineError, errorMessage, status           string
	closed, allowClose                          bool
	showLibrary, showTools                      bool
	libraryDrawer                               bool
	library                                     native.ListState
	settings                                    settingsState
	imageImport                                 importState
	renameID, renameValue                       string
	showRename                                  bool
	confirmOpen                                 bool
	confirmTitle, confirmMessage, confirmButton string
	confirmAction                               func()
	pvOpen                                      bool
	sourceOpen                                  bool
	sourceZoom                                  float32
	sourceX, sourceY                            float32
	preferencesMu                               sync.Mutex
	preferencesSeq                              atomic.Uint64
	loadDone                                    chan struct{}
	loadedEngine                                *engine.Client
	tray                                        *mygo.Tray
}

func New(dataDir, resourcesDir string) *App {
	return &App{dataDir: dataDir, resourcesDir: resourcesDir, prefs: preferences.Defaults(), loading: true, showLibrary: true, showTools: true, sourceZoom: 1, loadDone: make(chan struct{})}
}

// Shutdown joins the helper after the native event loop stops. It also handles
// quitting during initial loading, before the helper has reached the UI thread.
func (a *App) Shutdown() {
	<-a.loadDone
	if a.jobs != nil {
		_ = a.jobs.Interrupt()
	}
	if a.loadedEngine != nil {
		_ = a.loadedEngine.Close()
	}
	a.preferencesSeq.Add(1)
	a.preferencesMu.Lock()
	defer a.preferencesMu.Unlock()
	if !a.loading {
		_ = preferences.Save(filepath.Join(a.dataDir, "preferences.json"), a.prefs)
	}
}

func (a *App) Attach(win *mygo.Window) {
	a.win = win
	a.installMenu()
	win.OnClose(func(e *mygo.CloseEvent) {
		if a.allowClose {
			return
		}
		if a.editor != nil && a.editor.dirty {
			e.PreventDefault()
			a.confirm("放弃尚未保存的摆棋？", "已保存的残局与分支会保留。", "放弃并关闭", func() { a.editor.dirty = false; win.Close() })
			win.Invalidate()
			return
		}
		if a.jobs != nil {
			if r := a.jobs.Record(); r != nil && r.Status == "running" {
				if a.ensureTray() {
					e.PreventDefault()
					if a.research != nil {
						a.research.Leave()
					}
					win.Hide()
					return
				}
				e.PreventDefault()
				a.errorMessage = "图片仍在识别，系统托盘不可用。请等待完成或明确停止后关闭。"
				win.Invalidate()
				return
			}
		}
	})
	win.OnClosed(func() {
		a.closed = true
		if a.research != nil {
			a.research.Leave()
		}
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.allowClose && a.editor != nil && a.editor.dirty {
			e.PreventDefault()
			win.Show()
			a.confirm("退出并放弃尚未保存的摆棋？", "已保存的残局与分支会保留。", "退出", func() { a.allowClose = true; mygo.App.Quit() })
			win.Invalidate()
			return
		}
		if a.jobs != nil {
			_ = a.jobs.Interrupt()
		}
	})
	mygo.Power.OnSuspend(func() {
		a.update(func() {
			if a.research != nil {
				a.research.Stop()
			}
		})
	})
	mygo.App.OnDidBecomeActive(func() {
		if !a.closed {
			win.Show()
			win.Focus()
		}
	})
	go a.load()
}

func (a *App) update(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(func() {
		if !a.closed {
			fn()
		}
	})
}

func resolveWorker(resources string) string {
	name := "xiangqi-engine"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, p := range []string{filepath.Join(resources, "bin", name), filepath.Join(resources, runtime.GOOS+"-"+runtime.GOARCH, "bin", name)} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return filepath.Join(resources, "bin", name)
}

func (a *App) load() {
	defer close(a.loadDone)
	p, perr := preferences.Load(filepath.Join(a.dataDir, "preferences.json"))
	s, err := store.New(filepath.Join(a.dataDir, "Studies"))
	if err != nil {
		a.update(func() { a.loading = false; a.errorMessage = "残局库读取失败：" + err.Error() })
		return
	}
	j, jerr := recognition.NewJobs(filepath.Join(a.dataDir, "Recognition"))
	e, eerr := engine.New(resolveWorker(a.resourcesDir), filepath.Join(a.resourcesDir, "pikafish.nnue"))
	a.loadedEngine = e
	a.update(func() {
		a.prefs = p
		a.store = s
		a.studies = s.List()
		a.jobs = j
		a.engine = e
		a.loading = false
		if perr != nil {
			a.errorMessage = "设置读取失败，已采用默认值：" + perr.Error()
		}
		if jerr != nil {
			a.errorMessage = "图片识别任务读取失败：" + jerr.Error()
		}
		if eerr != nil {
			a.engineError = "离线引擎尚未就绪：" + eerr.Error()
		}
		if j != nil {
			j.SetOnChange(func() { a.update(func() { a.jobChanged() }) })
		}
		for _, study := range a.studies {
			if study.ID == p.CurrentStudy && !study.IsDraft {
				a.openStudy(study)
				return
			}
		}
		if len(a.studies) > 0 && !a.studies[0].IsDraft {
			a.openStudy(a.studies[0])
		}
	})
}

func (a *App) savePreferences() {
	p := a.prefs
	seq := a.preferencesSeq.Add(1)
	go func() {
		a.preferencesMu.Lock()
		defer a.preferencesMu.Unlock()
		if seq != a.preferencesSeq.Load() {
			return
		}
		if err := preferences.Save(filepath.Join(a.dataDir, "preferences.json"), p); err != nil {
			a.update(func() { a.errorMessage = "设置保存失败：" + err.Error() })
		}
	}()
}

func (a *App) refreshLibrary() {
	if a.store != nil {
		a.studies = a.store.List()
	}
}

func (a *App) guardEdit(fn func()) {
	if a.editor != nil && a.editor.saving {
		return
	}
	if a.editor != nil && a.editor.dirty {
		a.confirm("放弃尚未保存的摆棋？", "保存后可以从残局库再次打开。", "放弃修改", fn)
		return
	}
	fn()
}

func (a *App) openStudy(study domain.Study) {
	if study.IsDraft {
		a.beginEdit(study)
		return
	}
	if a.research != nil {
		a.research.Leave()
	}
	a.research = nil
	a.editor = nil
	a.board = chessboard.State{}
	a.status = ""
	a.prefs.CurrentStudy = study.ID
	if a.engine == nil {
		a.errorMessage = a.engineError
		return
	}
	s := session.New(study, a.engine, func(s domain.Study) error {
		err := a.store.SaveEdited(s)
		if err == nil {
			a.refreshLibrary()
		}
		return err
	})
	s.Dispatch = a.update
	s.OnChange = func() {
		if s.Error != "" {
			a.status = s.Error
		}
	}
	s.ShowEvaluation = a.prefs.ShowEvaluation
	s.ShowBranchScores = a.prefs.ShowBranchScores
	s.ShowLights = a.prefs.ShowLights
	s.Budget = a.prefs.Budget
	a.research = s
	s.Activate()
	a.savePreferences()
}

func (a *App) beginEdit(study domain.Study) {
	if a.research != nil {
		a.research.Leave()
		a.research = nil
	}
	a.board = chessboard.State{}
	a.editor = &editorState{original: study, board: domain.NewEditor(study.InitialPieces), name: study.Name, side: study.InitialSide, bottom: study.BottomSide}
	a.status = "选择棋子放置，或选中棋盘棋子调整"
}

func (a *App) newStudy() {
	if a.store == nil {
		return
	}
	a.guardEdit(func() { a.beginEdit(domain.NewStudy("新残局", nil, domain.Red)); a.editor.dirty = true })
}

func (a *App) editCurrent() {
	if a.research != nil {
		study := a.research.Study
		a.guardEdit(func() { a.beginEdit(study) })
	}
}

func (a *App) saveEditor(start bool) {
	ed := a.editor
	if ed == nil || ed.saving || a.store == nil {
		return
	}
	if a.engine == nil {
		a.errorMessage = a.engineError
		return
	}
	result := ed.original.EditSetup(ed.name, append([]domain.Piece(nil), ed.board.Pieces...), ed.side, ed.bottom)
	ed.saving = true
	ed.message = "正在检查局面…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, err := a.engine.Inspect(ctx, result.InitialFEN(), nil, false)
		if err == nil && start && r.Error != "" {
			a.update(func() {
				if a.editor == ed {
					ed.saving = false
					ed.message = friendlyRule(r.Error)
				}
			})
			return
		}
		if err == nil {
			result.IsDraft = r.Error != ""
			err = a.store.SaveEdited(result)
		}
		a.update(func() {
			if a.editor != ed {
				return
			}
			ed.saving = false
			if err != nil {
				ed.message = ""
				a.errorMessage = "保存失败：" + err.Error()
				return
			}
			a.refreshLibrary()
			ed.dirty = false
			ed.message = "已保存"
			ed.original = result
			ed.image = nil
			ed.notes = ""
			a.sourceOpen = false
			a.status = "已保存"
			if start {
				a.openStudy(result)
			} else {
				a.editor = nil
				if !result.IsDraft {
					a.openStudy(result)
				}
			}
		})
	}()
}

func (a *App) rename(study domain.Study) {
	a.renameID = study.ID
	a.renameValue = study.Name
	a.showRename = true
}
func (a *App) commitRename() {
	for _, study := range a.studies {
		if study.ID == a.renameID {
			study.Name = a.renameValue
			if study.Name == "" {
				study.Name = "未命名残局"
			}
			study.ModifiedAt = domain.NewDate(time.Now())
			if err := a.store.Save(study); err != nil {
				a.errorMessage = err.Error()
				return
			}
			if a.research != nil && a.research.Study.ID == study.ID {
				a.research.Study.Name = study.Name
			}
			a.refreshLibrary()
			a.showRename = false
			return
		}
	}
}

func (a *App) copy(study domain.Study) {
	if err := a.store.Save(study.Duplicate()); err != nil {
		a.errorMessage = "复制失败：" + err.Error()
	} else {
		a.refreshLibrary()
	}
}
func (a *App) delete(study domain.Study) {
	a.confirm("删除“"+study.Name+"”？", "该残局及全部推演分支将被删除。", "删除残局", func() {
		if err := a.store.Delete(study.ID); err != nil {
			a.errorMessage = "删除失败：" + err.Error()
			return
		}
		if a.research != nil && a.research.Study.ID == study.ID {
			a.research.Leave()
			a.research = nil
		}
		if a.editor != nil && a.editor.original.ID == study.ID {
			a.editor = nil
		}
		a.refreshLibrary()
	})
}
func (a *App) confirm(title, message, button string, fn func()) {
	a.confirmOpen = true
	a.confirmTitle = title
	a.confirmMessage = message
	a.confirmButton = button
	a.confirmAction = fn
}

func friendlyRule(s string) string {
	if s == "" {
		return ""
	}
	// The bridge may already return a translated validation message.
	return "局面无法推演，请检查摆子与先行方。\n" + s
}

func (a *App) ensureTray() bool {
	if a.tray != nil {
		return true
	}
	b, err := os.ReadFile(filepath.Join(a.resourcesDir, "icon.png"))
	if err != nil {
		return false
	}
	m := mygo.NewMenu([]*mygo.MenuItem{{Label: "打开象棋残局", Click: func(*mygo.MenuItem, *mygo.Window) { a.win.Show(); a.win.Focus() }}, {Label: "停止图片识别", Click: func(*mygo.MenuItem, *mygo.Window) {
		if a.jobs != nil {
			_ = a.jobs.Stop()
		}
		a.win.Show()
	}}, mygo.Separator(), {Role: mygo.RoleQuit}})
	t, err := mygo.NewTray(mygo.TrayOptions{Icon: b, ToolTip: "象棋残局 · 图片识别中", Menu: m})
	if err != nil {
		return false
	}
	a.tray = t
	return true
}

func (a *App) jobChanged() {
	if a.jobs == nil {
		return
	}
	r := a.jobs.Record()
	if r == nil {
		return
	}
	if r.Status == "ready" {
		a.status = "图片识别已完成，等待导入并校正"
		if a.tray != nil {
			a.tray.SetToolTip("图片识别已完成 · 待校正")
		}
		if a.win != nil {
			a.win.Show()
		}
	}
	if r.Status == "failed" {
		a.status = "图片识别失败：" + r.Error
		if a.win != nil {
			a.win.Show()
		}
	}
}

func (a *App) installMenu() {
	click := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { fn(); a.win.Invalidate() }
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "文件", Submenu: []*mygo.MenuItem{{Label: "新建残局", Accelerator: "CmdOrCtrl+N", Click: click(a.newStudy)}, {Label: "保存", Accelerator: "CmdOrCtrl+S", Click: click(func() { a.saveEditor(false) })}, {Label: "从图片新建", Click: click(a.newFromImage)}, {Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: click(a.openSettings)}, mygo.Separator(), {Role: mygo.RoleClose}, {Role: mygo.RoleQuit}}},
		{Role: mygo.RoleEditMenu},
		{Label: "研究", Submenu: []*mygo.MenuItem{{Label: "编辑研究起点", Click: click(a.editCurrent)}, {Label: "回退", Accelerator: "Alt+Left", Click: click(func() {
			if a.research != nil {
				a.research.Back()
			}
		})}, {Label: "前进", Accelerator: "Alt+Right", Click: click(func() { a.forward() })}, {Label: "AI 建议", Accelerator: "CmdOrCtrl+J", Click: click(func() {
			if a.research != nil {
				a.research.Recommend()
			}
		})}, {Label: "翻转棋盘", Accelerator: "CmdOrCtrl+Shift+F", Click: click(a.flip)}}},
		{Role: mygo.RoleWindowMenu},
	}))
}

func (a *App) forward() {
	if a.research != nil && !a.research.Forward() {
		a.research.OpenBranches(a.research.Study.CurrentID)
	}
}
func (a *App) flip() {
	if a.editor != nil {
		a.editor.bottom = a.editor.bottom.Opponent()
		a.editor.dirty = true
	} else if a.research != nil {
		a.research.Flip()
	}
}

func (a *App) statusText() string {
	if a.loading {
		return "正在打开残局库…"
	}
	if a.engineError != "" {
		return a.engineError
	}
	if a.editor != nil {
		if a.editor.message != "" {
			return a.editor.message
		}
		if a.editor.dirty {
			return "摆棋尚未保存"
		}
	}
	if a.research != nil && !a.research.Saved {
		return "研究尚未保存"
	}
	if a.status != "" {
		return a.status
	}
	return fmt.Sprintf("本地残局 · %d 个 · 离线 Pikafish", len(a.studies))
}
