//go:build !linux

package main

import (
	"runtime"

	"github.com/getlantern/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) startTray() {
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		// SetTitle is macOS-only; SetTooltip is Windows-only.
		if runtime.GOOS == "darwin" {
			systray.SetTitle("Phaze")
		}

		open := systray.AddMenuItem("Open Phaze", "Show the Phaze window")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit", "Quit Phaze")

		go func() {
			for {
				select {
				case <-open.ClickedCh:
					a.showWindow()
				case <-quit.ClickedCh:
					systray.Quit()
					wailsruntime.Quit(a.ctx)
				}
			}
		}()
	}, nil)
}

func (a *App) stopTray() {
	systray.Quit()
}

func (a *App) hideOnClose() bool {
	wailsruntime.WindowHide(a.ctx)
	return true
}

func (a *App) updateTrayUnread(_ int) {}
