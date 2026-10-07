package main

import (
	"flag"
	"log"
	"path/filepath"

	appui "github.com/ch1y1z1/xiangqi_go/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	data := flag.String("data-dir", "", "Use a separate local study directory")
	resources := flag.String("resources-dir", "", "Override bundled resources")
	flag.Parse()
	mygo.App.SetName("象棋残局")
	mygo.Theme.SetSource(mygo.ThemeLight)
	var app *appui.App
	mygo.App.WhenReady(func() {
		if *data == "" {
			base, err := mygo.App.Path(mygo.PathAppData)
			if err != nil {
				log.Fatal(err)
			}
			*data = filepath.Join(base, "com.chiyizi.xiangqi.desktop")
		}
		if *resources == "" {
			var err error
			*resources, err = mygo.App.Path(mygo.PathResources)
			if err != nil {
				log.Fatal(err)
			}
		}
		app = appui.New(*data, *resources)
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
	if app != nil {
		app.Shutdown()
	}
	if err != nil {
		log.Fatal(err)
	}
}
