package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ch1y1z1/xiangqi_go/internal/credentials"
	"github.com/ch1y1z1/xiangqi_go/internal/preferences"
	"github.com/ch1y1z1/xiangqi_go/internal/recognition"
	native "github.com/egoist/mygo/ui"
)

type settingsState struct {
	open, saving bool
	draft        preferences.Settings
	keys         map[string]string
	remove       map[string]bool
	error        string
}

func keyService(provider string) string {
	if provider == recognition.DeepSeek {
		return credentials.DeepSeekService
	}
	return credentials.CustomService
}

func (a *App) openSettings() {
	if a.loading || a.settings.saving {
		return
	}
	a.settings = settingsState{open: true, draft: a.prefs, keys: map[string]string{}, remove: map[string]bool{}}
}

func (a *App) settingsDialog(c *native.Context) {
	s := &a.settings
	native.DialogBase(c, &s.open, func(back, panel native.Element) {
		w, h := c.Size()
		back.Background(ink.Alpha(.25))
		panel.Width(min(float32(620), w-48)).MaxHeight(h - 64).Padding(24).Gap(12).Radius(16).Background(paper)
		native.Row(c).AlignItems(native.Center).Children(func() {
			native.Text(c, "设置").FontSize(23).FontWeight(600).Grow(1)
			action(c, "关闭", false, s.saving, func() { s.open = false })
		})
		native.Scroll(c).MaxHeight(h - 220).FillWidth().Gap(10).Children(func() {
			section(c, "研究显示")
			native.Checkbox(c, &s.draft.ShowEvaluation, "当前局面评分").Disabled(s.saving)
			native.Checkbox(c, &s.draft.ShowBranchScores, "分支比较评分").Disabled(s.saving)
			native.Checkbox(c, &s.draft.ShowLights, "安全吃子红绿灯").Disabled(s.saving)
			rule(c)
			section(c, "图片识别服务")
			r := &s.draft.Recognition
			native.Row(c).Gap(20).Children(func() {
				native.Radio(c, &r.Provider, recognition.DeepSeek, "DeepSeek 官方").Disabled(s.saving)
				native.Radio(c, &r.Provider, recognition.Custom, "自定义服务").Disabled(s.saving)
			})
			if r.Provider == recognition.Custom {
				native.Row(c).Gap(18).Children(func() {
					native.Radio(c, &r.API, recognition.ChatCompletions, "Chat Completions").Disabled(s.saving)
					native.Radio(c, &r.API, recognition.Responses, "Responses").Disabled(s.saving)
				})
				section(c, "API 地址")
				native.TextInput(c, &r.Address).Placeholder("https://example.com/v1").FillWidth().Label("API 地址").Disabled(s.saving)
				section(c, "视觉模型")
				native.TextInput(c, &r.Model).Placeholder("支持图片与 JSON 输出的模型").FillWidth().Label("模型名称").Disabled(s.saving)
			} else {
				native.Text(c, "deepseek-v4-flash · 官方图片接口").TextColor(muted).FontSize(13)
			}
			section(c, "API 密钥")
			key := s.keys[r.Provider]
			if native.TextInput(c.Key(r.Provider), &key).Password().Placeholder("填写新密钥；留空保留已保存密钥").FillWidth().Label("API 密钥").Disabled(s.saving).Changed() {
				s.keys[r.Provider] = key
				s.remove[r.Provider] = false
			}
			action(c, "移除此服务的密钥", false, s.saving, func() { s.keys[r.Provider] = ""; s.remove[r.Provider] = true })
			if s.remove[r.Provider] {
				native.Text(c, "点击保存设置后移除；取消保留原密钥。").TextColor(red).FontSize(12)
			}
			section(c, "思考强度")
			thinking := &r.DeepSeekThinking
			if r.Provider == recognition.Custom {
				thinking = &r.CustomThinking
			}
			native.Row(c).Gap(8).Children(func() {
				if r.Provider == recognition.Custom {
					action(c, "服务默认", *thinking == "", s.saving, func() { *thinking = "" })
				}
				for _, v := range []struct{ value, label string }{{"off", "关闭"}, {"low", "低"}, {"high", "高"}, {"max", "最高"}} {
					v := v
					action(c, v.label, *thinking == v.value, s.saving, func() { *thinking = v.value })
				}
			})
			native.Text(c, "更多思考会增加等待和额度消耗，识别结果仍需校正。").TextColor(muted).FontSize(12)
			if endpoint, err := r.Endpoint(); err == nil {
				native.Text(c, "请求地址："+endpoint).FontSize(12).TextColor(muted)
			}
			rule(c)
			section(c, "关于")
			native.Text(c, "象棋残局 0.1.0 · mygo Native UI\nPikafish 离线引擎 · 本地 JSON 残局\n代码 GPL-3.0；NNUE 使用遵循上游独立条款。").FontSize(12).TextColor(muted)
		})
		if s.error != "" {
			native.Text(c, s.error).TextColor(red).FontSize(13)
		}
		native.Row(c).Gap(10).Justify(native.End).Children(func() {
			action(c, "取消", false, s.saving, func() { s.open = false })
			label := "保存设置"
			if s.saving {
				label = "正在保存…"
			}
			action(c, label, true, s.saving, a.saveSettings)
		})
	})
}

func (a *App) saveSettings() {
	s := &a.settings
	if s.saving {
		return
	}
	p := s.draft
	if err := p.Recognition.Validate("configured-key"); err != nil {
		s.error = err.Error()
		return
	}
	keys := map[string]string{}
	removes := map[string]bool{}
	for k, v := range s.keys {
		keys[k] = strings.TrimSpace(v)
	}
	for k, v := range s.remove {
		removes[k] = v
	}
	s.saving = true
	s.error = ""
	a.preferencesSeq.Add(1)
	go func() {
		var err error
		for _, provider := range []string{recognition.DeepSeek, recognition.Custom} {
			if removes[provider] {
				err = credentials.Delete(keyService(provider))
			} else if keys[provider] != "" {
				err = credentials.Set(keyService(provider), keys[provider])
			}
			if err != nil {
				break
			}
		}
		if err == nil {
			a.preferencesMu.Lock()
			err = preferences.Save(filepath.Join(a.dataDir, "preferences.json"), p)
			a.preferencesMu.Unlock()
		}
		a.update(func() {
			s.saving = false
			if err != nil {
				s.error = fmt.Sprintf("设置保存失败：%s", err)
				return
			}
			a.prefs = p
			s.keys = nil
			s.remove = nil
			s.open = false
			a.status = "设置已保存"
			if a.research != nil {
				a.research.SetShowEvaluation(p.ShowEvaluation)
				a.research.SetShowBranchScores(p.ShowBranchScores)
				a.research.SetShowLights(p.ShowLights)
			}
		})
	}()
}
