package main

import (
	"embed"
	"encoding/json"
	"errors"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"tracker/desktop/internal/api"
	"tracker/desktop/internal/brand"
	"tracker/desktop/internal/config"
	"tracker/desktop/internal/secrets"
	"tracker/desktop/internal/tracker"
)

// The webview loads only these bundled assets (fonts included).
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cfg, err := config.NewStore()
	if err != nil {
		log.Fatal(err)
	}
	svc := tracker.New(cfg, secrets.OS{})

	app := application.New(application.Options{
		Name:        brand.Name,
		Description: "Personal project tracker",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: securityHeaders,
		},
		// Errors reach JavaScript as {kind, message} so the frontend can tell
		// "signed out" from "offline" from a validation message.
		MarshalError: func(err error) []byte {
			var e *api.Error
			if !errors.As(err, &e) {
				e = &api.Error{Kind: api.KindServer, Message: err.Error()}
			}
			b, _ := json.Marshal(e)
			return b
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:                      brand.Name,
		Width:                      1440,
		Height:                     900,
		MinWidth:                   720,
		MinHeight:                  560,
		BackgroundColour:           application.NewRGB(0x0D, 0x0E, 0x10),
		URL:                        "/",
		DefaultContextMenuDisabled: !devBuild,
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
