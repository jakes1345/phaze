package ui

import "runtime"

func IsMobile() bool {
	return runtime.GOOS == "android" || runtime.GOOS == "ios"
}
