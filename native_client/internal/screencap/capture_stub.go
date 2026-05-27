//go:build android || ios || (darwin && !cgo)

package screencap

import (
	"fmt"
	"image"
)

type Capturer struct{}

func New(fps int) *Capturer           { return &Capturer{} }
func (c *Capturer) Frames() <-chan image.Image { return make(chan image.Image) }
func (c *Capturer) Start() error      { return fmt.Errorf("screen capture not supported on this platform") }
func (c *Capturer) Stop()             {}
