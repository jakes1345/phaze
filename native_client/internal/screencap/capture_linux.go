//go:build linux && !android

package screencap

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os/exec"
	"sync"
	"time"
)

type Capturer struct {
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	frameCh chan image.Image
	fps     int
}

func New(fps int) *Capturer {
	if fps <= 0 {
		fps = 10
	}
	return &Capturer{
		fps:     fps,
		frameCh: make(chan image.Image, 2),
	}
}

func (c *Capturer) Frames() <-chan image.Image { return c.frameCh }

func (c *Capturer) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	c.running = true
	c.stopCh = make(chan struct{})
	go c.loop()
	return nil
}

func (c *Capturer) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return
	}
	c.running = false
	close(c.stopCh)
}

func (c *Capturer) loop() {
	interval := time.Second / time.Duration(c.fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			img, err := grabFrame()
			if err != nil {
				continue
			}
			select {
			case c.frameCh <- img:
			default:
			}
		}
	}
}

func grabFrame() (image.Image, error) {
	// Try xdpyinfo + import (X11 with ImageMagick)
	out, err := exec.Command("import", "-window", "root", "-silent", "png:-").Output()
	if err == nil && len(out) > 0 {
		return png.Decode(bytes.NewReader(out))
	}
	// Fallback: grim for Wayland (sway/wlroots)
	out, err = exec.Command("grim", "-").Output()
	if err == nil && len(out) > 0 {
		return png.Decode(bytes.NewReader(out))
	}
	return nil, fmt.Errorf("no screen capture method available (install ImageMagick or grim)")
}
