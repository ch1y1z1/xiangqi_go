package ui

import (
	"fmt"
	"strings"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/ui/chessboard"
	native "github.com/egoist/mygo/ui"
)

func (a *App) View(c *native.Context) {
	c.SetTheme(&theme)
	w, _ := c.Size()
	// Run actions after construction so saves see all bound edits and layout
	// switches cannot change the model while its controls are still being built.
	c.OnShortcut(native.Cmd, native.KeyN, a.newStudy)
	c.OnShortcut(native.Cmd, native.KeyS, func() { a.saveEditor(false) })
	c.OnShortcut(native.Cmd, native.KeyComma, a.openSettings)
	c.OnShortcut(native.Cmd|native.Shift, native.KeyF, a.flip)
	c.OnShortcut(native.Alt, native.KeyRight, a.forward)
	if a.research != nil {
		c.OnShortcut(native.Cmd, native.KeyJ, a.research.Recommend)
		c.OnShortcut(native.Alt, native.KeyLeft, a.research.Back)
		c.OnShortcut(native.Cmd, native.KeyHome, a.research.Root)
		c.OnShortcut(0, native.KeyEscape, a.research.Stop)
	}
	if a.editor != nil && !a.editor.saving {
		c.OnShortcut(native.Cmd, native.KeyZ, func() {
			a.editor.board.Undo()
			a.editor.dirty = true
		})
		c.OnShortcut(native.Cmd|native.Shift, native.KeyZ, func() {
			a.editor.board.Redo()
			a.editor.dirty = true
		})
	}
	a.syncWindowTitle()
	hasSidebar := !a.loading && a.showLibrary && w >= 1280
	// These layouts have different element hierarchies. Give each its own
	// identity so clipping, focus and paint state cannot cross between them.
	native.Box(c.Key(hasSidebar)).Fill().MinWidth(0).Children(func() {
		if hasSidebar {
			split := native.Split(c, &a.prefs.LeftWidth, func() { a.sidebarPane(c) }, func() {
				a.workspace(c, w-a.prefs.LeftWidth-1, true)
			}).Fill()
			if split.Changed() {
				a.prefs.LeftWidth = max(float32(220), min(float32(360), a.prefs.LeftWidth))
				a.savePreferences()
			}
		} else {
			a.workspace(c, w, false)
		}
	})
	a.dialogs(c)
}

func (a *App) sidebarPane(c *native.Context) {
	native.Column(c).Fill().Background(sidebar).Children(func() {
		a.sidebarTitleBar(c)
		native.Box(c).Grow(1).FillWidth().MinHeight(0).Children(func() { a.libraryView(c) })
		native.Text(c, "本地保存 · 离线研究").FontSize(11).TextColor(muted).Padding(6, 24).Height(28)
	})
}

func (a *App) workspace(c *native.Context, width float32, hasSidebar bool) {
	native.Column(c).Fill().MinWidth(0).Background(paper).Children(func() {
		a.workspaceTitleBar(c, width, hasSidebar)
		if a.loading {
			native.Column(c).Grow(1).FillWidth().Center().Gap(14).Children(func() { native.Spinner(c); native.Text(c, "正在打开本地残局库…").TextColor(muted) })
		} else if a.showTools {
			right := min(a.prefs.RightWidth, max(float32(300), width*0.28))
			first := max(float32(400), width-right)
			split := native.Split(c, &first, func() { a.center(c) }, func() { a.tools(c) }).Grow(1).FillWidth()
			if split.Changed() {
				a.prefs.RightWidth = max(float32(300), min(float32(520), width-first))
				a.savePreferences()
			}
		} else {
			a.center(c)
		}
		native.Row(c).Height(28).Shrink(0).Padding(5, 20).AlignItems(native.Center).Children(func() {
			native.Text(c, a.statusText()).FontSize(11).TextColor(muted).SingleLine().Grow(1).MinWidth(0)
			if !hasSidebar {
				native.Text(c, "本地保存 · 离线研究").FontSize(11).TextColor(muted)
			}
		})
	})
}

func (a *App) libraryView(c *native.Context) {
	studies := append([]domain.Study(nil), a.studies...)
	native.Column(c).Fill().Background(sidebar).Padding(14).Gap(10).Children(func() {
		native.Row(c).AlignItems(native.Center).Children(func() {
			native.Text(c, "我的残局").FontSize(13).FontWeight(600).TextColor(muted).Grow(1)
			native.Textf(c, "%d 个", len(studies)).TextColor(muted).FontSize(12)
		})
		sidebarCommand(c, iconNew, "新建残局", a.newStudy)
		a.library.Key = func(i int) any { return studies[i].ID }
		a.library.Label = func(i int) string { return studies[i].Name }
		native.List(c, &a.library, len(studies), func(i int) {
			study := studies[i]
			selected := a.research != nil && a.research.Study.ID == study.ID || a.editor != nil && a.editor.original.ID == study.ID
			row := native.Row(c.Key(study.ID)).Gap(8).Padding(10).Radius(12).MinHeight(100).AlignItems(native.Center)
			if selected {
				row.Background(sidebarSelected)
			} else if row.Hovered() {
				row.Background(sidebarHover)
			}
			row.ContextMenu(func(m *native.Menu) { a.studyMenu(m, study) })
			row.Children(func() {
				pieces := study.CurrentPieces()
				if study.IsDraft {
					pieces = study.InitialPieces
				}
				native.Box(c).Size(56, 72).Shrink(0).Clip().Draw(func(p *native.Painter, r native.Rect) {
					chessboard.Render(p, r, chessboard.Options{Pieces: pieces, Bottom: study.BottomSide})
				})
				button := native.ButtonBase(c).Grow(1).Shrink(1).MinWidth(0).Tooltip(study.Name).Children(func() {
					native.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
						native.Text(c, study.Name).FillWidth().FontSize(15).FontWeight(500).MaxLines(2)
						state, detail := "", ""
						if study.IsDraft {
							state = "草稿"
							detail = fmt.Sprintf("%d 枚棋子", len(study.InitialPieces))
						} else if len(study.Nodes) > 1 {
							state = "继续研究"
							detail = fmt.Sprintf("%d 手 · %d 分支", len(study.Line(study.CurrentID)), study.BranchCount())
						} else {
							state = study.InitialSide.Title() + "先行"
							detail = fmt.Sprintf("%d 枚棋子", len(study.InitialPieces))
						}
						native.Text(c, state).FillWidth().FontSize(12).TextColor(muted).SingleLine()
						native.Text(c, detail).FillWidth().FontSize(12).TextColor(muted).SingleLine().Tooltip(detail)
					})
				})
				button.OnClick(func() {
					a.guardEdit(func() { a.libraryDrawer = false; a.openStudy(study) })
				})
				moreMenu(c, study.Name+"的操作", func(m *native.Menu) { a.studyMenu(m, study) })
			})
		}).Grow(1).Gap(6).Children(func() {
			if len(studies) == 0 {
				native.Text(c, "还没有残局\n从一张空棋盘开始。").TextColor(muted).Padding(12)
			}
		})
		if a.jobs != nil {
			if r := a.jobs.Record(); r != nil {
				label := "图片识别未完成"
				if r.Status == "running" {
					label = "图片正在识别"
				} else if r.Status == "ready" {
					label = "图片已识别 · 待校正"
				}
				action(c, label, false, false, func() { a.imageImport.open = true }).FillWidth()
			}
		}
		sidebarCommand(c, iconImage, "从图片新建", a.newFromImage)
	})
}

func (a *App) studyMenu(m *native.Menu, study domain.Study) {
	if m.Item("修改名称").Chosen() {
		a.rename(study)
	}
	if m.Item("编辑棋子与名称").Chosen() {
		a.guardEdit(func() { a.libraryDrawer = false; a.beginEdit(study) })
	}
	if m.Item("复制").Chosen() {
		a.copy(study)
	}
	m.Separator()
	if m.Item("删除残局").Chosen() {
		a.delete(study)
	}
}

func (a *App) center(c *native.Context) {
	native.Column(c).Grow(1).FillHeight().MinWidth(0).Padding(18, 20, 14).Gap(10).Children(func() {
		if a.editor == nil && a.research == nil {
			native.Column(c).Grow(1).FillWidth().Center().Gap(18).Children(func() {
				native.Text(c, "摆一盘棋，慢慢研究。").FontSize(25)
				native.Text(c, "所有摆棋、保存与 Pikafish AI 均离线运行。").TextColor(muted)
				action(c, "创建残局", true, false, a.newStudy)
				if a.engineError != "" {
					native.Text(c, a.engineError).TextColor(red)
				}
			})
			return
		}
		if a.editor != nil {
			a.editorHeader(c)
		} else {
			a.studyHeader(c)
		}
		o := a.boardOptions()
		chessboard.View(c, &a.board, o).Grow(1).FillWidth().MinHeight(240)
		if a.editor == nil && a.research != nil && a.research.ShowLights {
			native.Row(c).Gap(5).AlignItems(native.Center).Children(func() {
				native.Text(c, "●").TextColor(theme.Success).FontSize(12)
				native.Text(c, "可安全吃").FontSize(12).TextColor(muted)
				native.Text(c, "●").TextColor(red).FontSize(12).Margin(0, 0, 0, 12)
				native.Text(c, "有被吃风险").FontSize(12).TextColor(muted)
				native.Box(c).Grow(1)
				native.Text(c, a.research.Study.BottomSide.Title()+"观察").FontSize(12).TextColor(muted)
			})
		}
		if a.editor != nil {
			a.editorFooter(c)
		} else {
			a.studyFooter(c)
		}
	})
}

func (a *App) boardOptions() chessboard.Options {
	if ed := a.editor; ed != nil {
		return chessboard.Options{Pieces: ed.board.Pieces, Bottom: ed.bottom, Selected: ed.board.Selected, Interactive: !ed.saving,
			OnTap: func(s domain.Square) {
				err := ed.board.Tap(s)
				ed.dirty = true
				ed.message = ""
				if err != nil {
					ed.message = err.Error()
				}
			},
			OnDrag: func(from, to domain.Square) {
				err := ed.board.Move(from, to)
				ed.dirty = true
				ed.message = ""
				if err != nil {
					ed.message = err.Error()
				}
			}, OnDelete: func() { ed.board.DeleteSelected(); ed.dirty = true },
			OnUndo: func() { ed.board.Undo(); ed.dirty = true }, OnRedo: func() { ed.board.Redo(); ed.dirty = true }, OnImageDrop: a.loadImage,
			OnPlace: func(piece chessboard.Placement, square domain.Square) {
				for _, p := range ed.board.Pieces {
					if p.Square == square {
						ed.message = "这个位置已有棋子，请选择空落点。"
						return
					}
				}
				ed.board.Choose(piece.Kind, piece.Side)
				if err := ed.board.Tap(square); err != nil {
					ed.message = err.Error()
					return
				}
				ed.dirty = true
				ed.message = ""
			}}
	}
	s := a.research
	o := chessboard.Options{Pieces: s.Study.CurrentPieces(), Bottom: s.Study.BottomSide, Selected: s.Selected, Captures: s.Rules.Captures, Interactive: !s.RulesBusy && !s.Rules.Finished && (s.AISide == "" || s.Paused || s.AISide != s.Study.SideToMove()), OnTap: s.Tap, OnDrag: s.Drag}
	if !s.ShowLights {
		o.Captures = nil
	}
	if s.Rules.Check {
		o.CheckedSide = s.Study.SideToMove()
	}
	if n := s.Study.Node(s.Study.CurrentID); n != nil {
		o.LastMove = n.Move
	}
	if s.Suggestion != nil {
		if m, err := domain.ParseMove(s.Suggestion.BestMove); err == nil {
			o.Recommendation = &m
		}
	}
	if s.Selected != nil {
		for _, uci := range s.Rules.LegalMoves {
			if m, err := domain.ParseMove(uci); err == nil && m.From == *s.Selected {
				o.Legal = append(o.Legal, m)
			}
		}
	}
	return o
}

func (a *App) editorHeader(c *native.Context) {
	ed := a.editor
	native.Row(c).Gap(10).AlignItems(native.Center).Children(func() {
		native.Text(c, "编辑").TextColor(muted)
		if native.TextInput(c, &ed.name).FontSize(19).Grow(1).Disabled(ed.saving).Label("残局名称").Changed() {
			ed.dirty = true
		}
		native.MenuButton(c, "工具", func(m *native.Menu) {
			if m.Item("从图片导入").Chosen() {
				a.imageImport.open = true
			}
			if m.Item("翻转棋盘").Chosen() {
				a.flip()
			}
			if ed.image != nil && m.Item("查看原图").Chosen() {
				a.sourceOpen = true
			}
		})
	})
	text := "自由摆棋 · 未完成的局面也可以保存"
	if len(ed.original.Nodes) > 1 {
		text = "编辑研究起点 · 修改摆子会保留编辑前副本"
	}
	native.Text(c, text).FontSize(12).TextColor(muted)
}

func (a *App) studyHeader(c *native.Context) {
	s := a.research
	state := s.Study.SideToMove().Title() + "行棋"
	if s.RulesBusy {
		state = "正在检查局面…"
	} else if s.Rules.Finished {
		state = s.Rules.Outcome
		if s.Rules.Winner != "" {
			state = domain.Side(s.Rules.Winner).Title() + "胜 · " + state
		}
	} else if s.Rules.Check {
		state += " · 将军"
	}
	native.Row(c).AlignItems(native.Center).Gap(10).Children(func() {
		native.Text(c, state).TextColor(muted).FontSize(13).SingleLine().Grow(1).MinWidth(0)
		if s.ShowEvaluation {
			native.Text(c, s.EvalText(s.Study.CurrentID)).TextColor(teal).FontSize(16).Description(s.EvalDetail(s.Study.CurrentID))
		}
		moreMenu(c, s.Study.Name+"的操作", func(m *native.Menu) {
			if m.Item("修改名称").Chosen() {
				a.rename(s.Study)
			}
			if m.Item("编辑棋子与名称").Chosen() {
				a.editCurrent()
			}
			if m.Item("翻转棋盘").Chosen() {
				s.Flip()
			}
			if m.Item("重新分析").Chosen() {
				s.Reanalyze()
			}
		})
	})
}

func (a *App) editorFooter(c *native.Context) {
	ed := a.editor
	native.Row(c).Gap(8).AlignItems(native.Center).Children(func() {
		action(c, "撤销", false, ed.saving || !ed.board.CanUndo(), func() { ed.board.Undo(); ed.dirty = true })
		action(c, "重做", false, ed.saving || !ed.board.CanRedo(), func() { ed.board.Redo(); ed.dirty = true })
		if ed.board.Selected != nil {
			action(c, "删除棋子", false, ed.saving, func() { ed.board.DeleteSelected(); ed.dirty = true })
		}
		native.Box(c).Grow(1)
		action(c, "取消", false, ed.saving, a.cancelEdit)
		action(c, "保存", false, ed.saving, func() { a.saveEditor(false) })
		action(c, "开始推演", true, ed.saving, func() { a.saveEditor(true) })
	})
	text := ed.message
	if text == "" {
		text = "选择托盘棋子，再点棋盘空落点"
	}
	native.Text(c, text).FontSize(12).TextColor(muted).MinHeight(22)
}

func (a *App) studyFooter(c *native.Context) {
	s := a.research
	native.Row(c).Gap(8).AlignItems(native.Center).Children(func() {
		action(c, "起点", false, s.Study.CurrentID == s.Study.RootID, s.Root)
		action(c, "回退", false, s.Study.CurrentID == s.Study.RootID, s.Back)
		n := s.Study.Node(s.Study.CurrentID)
		action(c, "前进", false, n == nil || len(n.Children) == 0, a.forward)
		native.Box(c).Grow(1)
		if s.Busy {
			action(c, "停止", true, false, s.Stop)
		} else if s.Suggestion != nil {
			action(c, "采用建议", true, false, s.Adopt)
		} else {
			action(c, "AI 建议", true, s.RulesBusy || s.Rules.Finished, s.Recommend)
		}
		label := "双方手动"
		if s.AISide != "" {
			label = "AI 执" + s.AISide.Title()
			if s.Paused {
				label += " · 已暂停"
			}
		}
		native.MenuButton(c, label, func(m *native.Menu) {
			if m.Item("双方手动").Checked(s.AISide == "").Chosen() {
				s.SetAI("")
			}
			if m.Item("我执红 / AI 执黑").Checked(s.AISide == domain.Black).Chosen() {
				s.SetAI(domain.Black)
			}
			if m.Item("我执黑 / AI 执红").Checked(s.AISide == domain.Red).Chosen() {
				s.SetAI(domain.Red)
			}
			if s.Paused && m.Item("恢复托管").Chosen() {
				s.ResumeAI()
			}
		})
	})
	label := "双方手动推演 · 回退改走保留分支"
	if s.Busy {
		label = "AI 正在思考…"
	} else if s.Paused {
		label = "托管已暂停，确认局面后选择恢复"
	} else if s.Suggestion != nil {
		label = "建议已就绪，点击采用才会落子"
	}
	native.Text(c, label).FontSize(12).TextColor(muted).MinHeight(22)
}

func (a *App) tools(c *native.Context) {
	native.Scroll(c).Fill().Padding(18).Gap(10).Background(card.Alpha(0.35)).Children(func() {
		if a.editor != nil {
			a.editorTools(c)
		} else if a.research != nil {
			a.studyTools(c)
		} else {
			native.Text(c, "选择一个残局开始研究").TextColor(muted)
		}
	})
}

func (a *App) editorTools(c *native.Context) {
	ed := a.editor
	native.Text(c, "摆棋工具").FontSize(16).FontWeight(500)
	for _, side := range []domain.Side{domain.Red, domain.Black} {
		section(c, side.Title()+"库存")
		for row := 0; row < 2; row++ {
			native.Row(c).Gap(6).Children(func() {
				for i := row * 4; i < min(row*4+4, len(domain.Kinds)); i++ {
					kind := domain.Kinds[i]
					remaining := kind.Limit()
					for _, piece := range ed.board.Pieces {
						if piece.Side == side && piece.Kind == kind {
							remaining--
						}
					}
					col := red
					if side == domain.Black {
						col = ink
					}
					b := native.ButtonBase(c).Grow(1).MinWidth(0).Height(67).Padding(4).Radius(10).Background(card).Disabled(remaining <= 0 || ed.saving).Children(func() {
						native.Column(c).Gap(2).Center().Children(func() {
							native.Text(c, kind.Glyph(side)).Font(chessboard.PieceFontFamily()).FontSize(27).TextColor(col)
							native.Textf(c, "剩余 %d", max(0, remaining)).FontSize(11).TextColor(muted)
						})
					})
					if remaining > 0 && !ed.saving {
						b.Drag(chessboard.Placement{Kind: kind, Side: side})
					}
					if ed.board.PlacementKind == kind && ed.board.PlacementSide == side && remaining > 0 {
						b.Border(1.5, teal)
					}
					b.OnClick(func() {
						ed.board.Choose(kind, side)
						ed.message = "放置" + side.Title() + kind.Glyph(side)
					})
				}
				for i := min(row*4+4, len(domain.Kinds)); i < (row+1)*4; i++ {
					native.Box(c).Grow(1)
				}
			})
		}
	}
	rule(c)
	section(c, "先行方")
	native.Row(c).Gap(10).Children(func() {
		if native.Radio(c, &ed.side, domain.Red, "红先").Disabled(ed.saving).Changed() {
			ed.dirty = true
		}
		if native.Radio(c, &ed.side, domain.Black, "黑先").Disabled(ed.saving).Changed() {
			ed.dirty = true
		}
	})
	section(c, "棋盘朝向")
	action(c, ed.bottom.Title()+"在下 · 点击切换", false, ed.saving, a.flip).FillWidth()
	native.Row(c).Gap(8).Children(func() {
		action(c, "初始盘", false, ed.saving, func() { ed.board.Replace(domain.InitialPieces()); ed.dirty = true }).Grow(1)
		action(c, "清空棋盘", false, ed.saving, func() { ed.board.Replace(nil); ed.dirty = true }).Grow(1)
	})
	rule(c)
	action(c, "从图片导入", false, ed.saving, func() { a.imageImport.open = true }).FillWidth()
	if ed.image != nil {
		action(c, "查看原图", false, false, func() { a.sourceOpen = true }).FillWidth()
		native.Image(c, ed.image).Height(165).FillWidth().Fit(native.Contain).Radius(10)
		if ed.notes != "" {
			native.Text(c, ed.notes).FontSize(12).TextColor(muted)
		}
	}
}

func (a *App) studyTools(c *native.Context) {
	s := a.research
	native.Text(c, "研究面板").FontSize(16).FontWeight(500)
	section(c, "当前线路")
	action(c, "起点", false, false, s.Root).FillWidth().ContextMenu(func(m *native.Menu) {
		if m.Item("回到起点").Chosen() {
			s.Root()
		}
		if n := s.Study.Node(s.Study.RootID); n != nil && len(n.Children) > 1 && m.Item("比较这里的分支").Chosen() {
			s.OpenBranches(n.ID)
		}
	})
	if len(s.Study.Line(s.Study.CurrentID)) == 0 {
		native.Text(c, "尚未落子").FontSize(12).TextColor(muted)
	}
	for i, n := range s.Study.Line(s.Study.CurrentID) {
		label := fmt.Sprintf("%d. %s", i+1, s.Study.Label(n))
		if len(n.Children) > 1 {
			label += " ⑂"
		}
		b := action(c.Key(n.ID), label, false, false, func() { s.Jump(n.ID) }).FillWidth().Tooltip(label)
		if n.ID == s.Study.CurrentID {
			b.Background(teal.Alpha(0.12)).TextColor(teal)
		}
		node := n
		b.ContextMenu(func(m *native.Menu) {
			if m.Item("跳到这一步").Chosen() {
				s.Jump(node.ID)
			}
			if len(node.Children) > 1 && m.Item("比较下一手分支").Chosen() {
				s.OpenBranches(node.ID)
			}
		})
	}
	parent := s.Study.Node(s.Study.CurrentID)
	if parent != nil && len(parent.Children) > 1 && s.BranchParentID == "" {
		action(c, fmt.Sprintf("比较 %d 条下一手分支", len(parent.Children)), false, false, func() { s.OpenBranches(parent.ID) }).FillWidth()
	}
	if s.BranchParentID != "" {
		rule(c)
		native.Row(c).Children(func() {
			native.Text(c, "下一手分支比较").FontWeight(600).Grow(1)
			action(c, "收起", false, false, s.CloseBranches)
		})
		if node := s.Study.Node(s.BranchParentID); node != nil {
			for _, id := range append([]string(nil), node.Children...) {
				n := s.Study.Node(id)
				if n == nil {
					continue
				}
				native.Column(c.Key(id)).Padding(12).Gap(5).Background(card).Radius(12).Children(func() {
					native.Row(c).Gap(8).Children(func() {
						native.Text(c, s.Study.Label(*n)).Grow(1)
						native.Text(c, s.EvalText(id)).TextColor(teal).Description(s.EvalDetail(id))
					})
					if text := s.Comparison(id, s.BranchParentID); text != "" {
						native.Text(c, text).FontSize(12).TextColor(muted)
					}
					action(c, "继续这条分支", false, false, func() { s.Jump(id) }).FillWidth()
				})
			}
		}
		action(c, "重新比较", false, false, s.Reanalyze).FillWidth()
	}
	rule(c)
	section(c, "AI 建议")
	native.Column(c).Padding(14).Gap(8).MinHeight(116).Background(teal.Alpha(0.07)).Radius(12).Children(func() {
		if s.Busy {
			native.Row(c).Gap(9).Children(func() { native.Spinner(c); native.Text(c, "正在思考…") })
		} else if s.Suggestion != nil {
			move, err := domain.ParseMove(s.Suggestion.BestMove)
			label := s.Suggestion.BestMove
			if err == nil {
				label = domain.Notation(move, s.Study.CurrentPieces())
			}
			native.Text(c, label).FontSize(22).FontWeight(600).TextColor(teal)
			native.Text(c, s.EvalText(s.Study.CurrentID)).FontSize(13).TextColor(muted)
			action(c, "查看参考变化", false, len(s.Suggestion.PV) == 0, func() { a.pvOpen = true })
		} else {
			native.Text(c, "请求建议后会显示推荐与评分").FontSize(13).TextColor(muted)
			native.Text(c, "采用才落子，参考变化不会改变研究。 ").FontSize(12).TextColor(muted)
		}
	})
	if s.Paused {
		action(c, "恢复 AI 托管", true, false, s.ResumeAI).FillWidth()
	}
	rule(c)
	section(c, "显示与分析")
	lights := s.ShowLights
	if native.Checkbox(c, &lights, "安全吃子红绿灯").Changed() {
		s.SetShowLights(lights)
		a.prefs.ShowLights = lights
		a.savePreferences()
	}
	current := s.ShowEvaluation
	if native.Checkbox(c, &current, "当前局面评分").Changed() {
		s.SetShowEvaluation(current)
		a.prefs.ShowEvaluation = current
		a.savePreferences()
	}
	branches := s.ShowBranchScores
	if native.Checkbox(c, &branches, "分支比较评分").Changed() {
		s.SetShowBranchScores(branches)
		a.prefs.ShowBranchScores = branches
		a.savePreferences()
	}
	section(c, "思考预算")
	native.Row(c).Gap(8).Children(func() {
		for _, option := range []struct {
			ms    int
			label string
		}{{300, "快速"}, {1000, "标准"}, {3000, "深入"}} {
			v := option
			action(c, v.label, s.Budget == v.ms, false, func() { s.SetBudget(v.ms); a.prefs.Budget = v.ms; a.savePreferences() }).Grow(1)
		}
	})
	native.Text(c, fmt.Sprintf("每次 %.1f 秒 · 评分固定红方视角", float64(s.Budget)/1000)).FontSize(12).TextColor(muted)
	action(c, "重新分析当前局面", false, s.RulesBusy, s.Reanalyze).FillWidth()
	if detail := s.EvalDetail(s.Study.CurrentID); detail != "" {
		native.Text(c, detail).FontSize(12).TextColor(muted)
	}
}

func (a *App) dialogs(c *native.Context) {
	native.DialogBase(c, &a.libraryDrawer, func(back, panel native.Element) {
		back.Background(ink.Alpha(0.25))
		panel.Width(360).MaxHeight(680).FillHeight().Background(paper).Radius(16)
		a.libraryView(c)
	})
	native.Modal(c, &a.showRename, func() {
		native.Text(c, "残局名称").FontSize(18).FontWeight(600)
		native.TextInput(c, &a.renameValue).Width(330).AutoFocus().Label("新名称").OnSubmit(a.commitRename)
		native.Row(c).Gap(10).Justify(native.End).Children(func() {
			action(c, "取消", false, false, func() { a.showRename = false })
			action(c, "保存名称", true, false, a.commitRename)
		})
	})
	native.Modal(c, &a.pvOpen, func() {
		native.Text(c, "参考变化").FontSize(19).FontWeight(600)
		native.Text(c, "只读预览 · 不会落子或新增研究分支").FontSize(12).TextColor(muted)
		if a.research != nil && a.research.Suggestion != nil {
			pieces := a.research.Study.CurrentPieces()
			native.Scroll(c).Width(440).MaxHeight(400).Gap(8).Children(func() {
				for i, uci := range a.research.Suggestion.PV {
					m, err := domain.ParseMove(uci)
					if err != nil {
						continue
					}
					native.Text(c, fmt.Sprintf("%d. %s", i+1, domain.Notation(m, pieces)))
					next := make([]domain.Piece, 0, len(pieces))
					for _, p := range pieces {
						if p.Square == m.To {
							continue
						}
						if p.Square == m.From {
							p.Square = m.To
						}
						next = append(next, p)
					}
					pieces = next
				}
			})
		}
		action(c, "关闭", false, false, func() { a.pvOpen = false })
	})
	a.importDialog(c)
	a.sourceDialog(c)
	a.settingsDialog(c)
	if a.confirmOpen {
		which := native.AlertDialog(c, &a.confirmOpen, a.confirmTitle, a.confirmMessage, "取消", a.confirmButton)
		if which == 1 {
			fn := a.confirmAction
			a.confirmAction = nil
			if fn != nil {
				fn()
			}
		}
	}
	open := a.errorMessage != ""
	if open {
		which := native.AlertDialog(c, &open, "无法完成操作", a.errorMessage, "知道了")
		if which == 0 {
			a.errorMessage = ""
		}
	}
}

func (a *App) cancelEdit() {
	a.guardEdit(func() {
		ed := a.editor
		a.editor = nil
		a.imageImport = importState{}
		a.sourceOpen = false
		a.status = "已取消编辑"
		if ed != nil && !ed.original.IsDraft {
			for _, study := range a.studies {
				if study.ID == ed.original.ID {
					a.openStudy(study)
					break
				}
			}
		}
	})
}
func (a *App) newFromImage() {
	if a.store == nil {
		return
	}
	a.guardEdit(func() {
		a.beginEdit(domain.NewStudy("新残局", nil, domain.Red))
		a.editor.dirty = true
		a.imageImport.open = true
	})
}

func shortName(s string) string { return strings.TrimSpace(s) }
