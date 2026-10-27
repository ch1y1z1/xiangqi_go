package main

import (
	"flag"
	"log"
	"log/slog"
	"path/filepath"

	"github.com/ch1y1z1/xiangqi_go/internal/diagnostics"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
	appui "github.com/ch1y1z1/xiangqi_go/internal/ui"
	"github.com/ch1y1z1/xiangqi_go/internal/ui/chessboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updatenative "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func main() {
	data := flag.String("data-dir", "", "Use a separate local study directory")
	resources := flag.String("resources-dir", "", "Override bundled resources")
	flag.Parse()
	mygo.App.SetName("象棋残局")
	logDir, logErr := mygo.App.Path(mygo.PathLogs)
	if *data != "" {
		// Keep updater preferences and window state isolated with development data.
		mygo.App.SetPath(mygo.PathUserData, *data)
		logDir, logErr = filepath.Join(*data, "logs"), nil
	}
	var logs *diagnostics.Session
	if logErr == nil {
		logs, logErr = diagnostics.Start(logDir, diagnostics.Info{Version: mygo.App.Version(), EngineCommit: engine.EngineCommit, NetworkSHA256: engine.NetworkSHA256})
	}
	if logErr != nil {
		diagnostics.Error("initialize_diagnostics", logErr)
	} else {
		defer logs.Close()
	}
	mygo.Use(updatenative.New(updater.Options{Language: "zh-Hans"}))
	mygo.Theme.SetSource(mygo.ThemeLight)
	var app *appui.App
	mygo.App.WhenReady(func() {
		if *data == "" {
			base, err := mygo.App.Path(mygo.PathAppData)
			if err != nil {
				diagnostics.Error("resolve_data_directory", err)
				log.Fatal("无法读取应用数据目录")
			}
			*data = filepath.Join(base, "com.chiyizi.xiangqi.desktop")
		}
		if *resources == "" {
			var err error
			*resources, err = mygo.App.Path(mygo.PathResources)
			if err != nil {
				diagnostics.Error("resolve_resources_directory", err)
				log.Fatal("无法读取应用资源目录")
			}
		}
		chessboard.LoadSystemPieceFont()
		app = appui.New(*data, *resources)
		app.SetDiagnostics(logs, logErr)
		area := mygo.Screen.PrimaryDisplay().WorkArea
		width, height := min(1440, area.Width-64), min(900, area.Height-64)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "象棋残局", Width: max(960, width), Height: max(680, height),
			MinWidth: 960, MinHeight: 680, StateKey: "main",
			TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: 56,
			TrafficLightPosition: &mygo.Point{X: 16, Y: 21},
			BackgroundColor:      "#FAF9F6", Content: ui.View(app.View),
		})
		app.Attach(win)
	})
	err := mygo.App.Run()
	diagnostics.Error("application_event_loop", err)
	if app != nil {
		app.Shutdown()
	}
	if err != nil {
		log.Fatal("应用事件循环异常结束")
	}
	slog.Info("application_stopped")
	if logs != nil {
		diagnostics.Error("finish_diagnostics", logs.Finish())
	}
}
