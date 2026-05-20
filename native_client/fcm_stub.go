//go:build !android

package main

// readFCMToken is a no-op on non-Android platforms.
func readFCMToken() string { return "" }
