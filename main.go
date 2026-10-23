// Ankh is a macOS workbench for CLI coding agents: projects as tabs, the agent
// in a real terminal, a file tree and GitLab-style review of the agent's changes.
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"ankh-ide/internal/agent"
	"ankh-ide/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

// dockIcon is the logo with its own rounded corners, shown in the Dock while
// the app runs; appicon.png is full-bleed for the bundle, macOS masks it.
//
//go:embed build/dockicon.png
var dockIcon []byte

func main() {
	// The same binary handles Claude Code hooks and statusLine.
	if len(os.Args) > 1 && (os.Args[1] == "hook" || os.Args[1] == "statusline") {
		os.Exit(agent.RunCLI(os.Args[1:], os.Stdin, os.Stdout))
	}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	cfgDir = filepath.Join(cfgDir, "Ankh")
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}

	host := &app.Host{}
	svc, err := app.New(host, cfgDir, exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ankh:", err)
		os.Exit(1)
	}

	wapp := application.New(application.Options{
		Name:        "Ankh",
		Description: "Workbench for CLI coding agents",
		Icon:        dockIcon,
		Services:    []application.Service{application.NewService(svc)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: func() { app.Shutdown(svc) },
	})

	var win *application.WebviewWindow
	host.Emit = wapp.Event.Emit
	host.SetClipboard = wapp.Clipboard.SetText
	host.SetBadge = setDockBadge
	host.PickFolder = func() (string, error) {
		d := wapp.Dialog.OpenFile().
			CanChooseDirectories(true).
			CanChooseFiles(false).
			SetTitle("Открыть проект")
		if win != nil {
			d = d.AttachToWindow(win)
		}
		return d.PromptForSingleSelection()
	}

	win = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Ankh",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(20, 22, 26),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
		},
		URL: "/",
	})

	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		setDockIcon(dockIcon)
	})

	app.Start(svc)
	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}
