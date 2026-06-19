package main

import (
	"context"
	_ "embed"
	"sync/atomic"

	"github.com/gen2brain/beeep"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed assets/icon.png
var trayIcon []byte

type App struct {
	ctx    context.Context
	unread atomic.Int32
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.startTray()
}

func (a *App) shutdown(_ context.Context) {
	a.stopTray()
}

func (a *App) beforeClose(_ context.Context) bool {
	return a.hideOnClose()
}

func (a *App) showWindow() {
	wailsruntime.WindowShow(a.ctx)
	wailsruntime.WindowUnminimise(a.ctx)
	wailsruntime.WindowSetAlwaysOnTop(a.ctx, true)
	wailsruntime.WindowSetAlwaysOnTop(a.ctx, false)
}

// WindowMinimise, WindowToggleMaximise, WindowClose — called from the custom title bar.
func (a *App) WindowMinimise()       { wailsruntime.WindowMinimise(a.ctx) }
func (a *App) WindowToggleMaximise() { wailsruntime.WindowToggleMaximise(a.ctx) }
func (a *App) WindowClose() {
	if a.hideOnClose() {
		return
	}
	wailsruntime.Quit(a.ctx)
}

// Notify sends a native OS desktop notification.
// Called from JavaScript: window.go.main.App.Notify(title, body)
func (a *App) Notify(title, body string) {
	_ = beeep.Notify(title, body, "")
}

// SetUnread updates the tray tooltip with the current unread count.
// Called from JavaScript when the unread count changes.
func (a *App) SetUnread(count int) {
	a.unread.Store(int32(count))
	a.updateTrayUnread(count)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
