//go:build windows

package screencap

import (
	"fmt"
	"image"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	gdi32           = syscall.NewLazyDLL("gdi32.dll")
	getDC           = user32.NewProc("GetDC")
	releaseDC       = user32.NewProc("ReleaseDC")
	getSystemMetrics = user32.NewProc("GetSystemMetrics")
	createCompatDC  = gdi32.NewProc("CreateCompatibleDC")
	createCompatBmp = gdi32.NewProc("CreateCompatibleBitmap")
	selectObject    = gdi32.NewProc("SelectObject")
	bitBlt          = gdi32.NewProc("BitBlt")
	getDIBits       = gdi32.NewProc("GetDIBits")
	deleteObject    = gdi32.NewProc("DeleteObject")
	deleteDC        = gdi32.NewProc("DeleteDC")
)

const (
	smCxScreen = 0
	smCyScreen = 1
	srcCopy    = 0x00CC0020
	biRGB      = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

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
	w, _, _ := getSystemMetrics.Call(smCxScreen)
	h, _, _ := getSystemMetrics.Call(smCyScreen)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("screen size zero")
	}
	width := int(w)
	height := int(h)

	hdc, _, _ := getDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer releaseDC.Call(0, hdc)

	memDC, _, _ := createCompatDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer deleteDC.Call(memDC)

	bmp, _, _ := createCompatBmp.Call(hdc, uintptr(width), uintptr(height))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer deleteObject.Call(bmp)

	selectObject.Call(memDC, bmp)
	bitBlt.Call(memDC, 0, 0, uintptr(width), uintptr(height), hdc, 0, 0, srcCopy)

	bi := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    int32(width),
		Height:   -int32(height),
		Planes:   1,
		BitCount: 32,
	}

	pixels := make([]byte, width*height*4)
	getDIBits.Call(memDC, bmp, 0, uintptr(height),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bi)),
		biRGB)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(pixels); i += 4 {
		img.Pix[i+0] = pixels[i+2] // R (BGRA→RGBA)
		img.Pix[i+1] = pixels[i+1] // G
		img.Pix[i+2] = pixels[i+0] // B
		img.Pix[i+3] = 255         // A
	}
	return img, nil
}
