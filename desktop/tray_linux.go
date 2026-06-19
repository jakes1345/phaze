//go:build linux

package main

func (a *App) startTray()              {}
func (a *App) stopTray()               {}
func (a *App) hideOnClose() bool       { return false }
func (a *App) updateTrayUnread(_ int)  {}
