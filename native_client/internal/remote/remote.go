package remote

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// InputEvent is sent over the DataChannel from the viewer to the host.
type InputEvent struct {
	Type   string  `json:"type"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Button int     `json:"button,omitempty"`
	Key    string  `json:"key,omitempty"`
	Code   string  `json:"code,omitempty"`
	Shift  bool    `json:"shift,omitempty"`
	Ctrl   bool    `json:"ctrl,omitempty"`
	Alt    bool    `json:"alt,omitempty"`
}

// ScreenSize returns the primary display resolution.
func ScreenSize() (w, h int, err error) {
	switch runtime.GOOS {
	case "linux":
		out, e := exec.Command("xdpyinfo").Output()
		if e != nil {
			return 1920, 1080, nil
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "dimensions:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					dim := strings.Split(parts[1], "x")
					if len(dim) == 2 {
						w, _ = strconv.Atoi(dim[0])
						h, _ = strconv.Atoi(dim[1])
						return w, h, nil
					}
				}
			}
		}
		return 1920, 1080, nil
	case "windows":
		return 1920, 1080, nil
	default:
		return 1920, 1080, nil
	}
}

// InjectInput processes an input event from the remote viewer and injects it
// into the host OS. Coordinates are normalized 0-1.
func InjectInput(evt InputEvent) error {
	sw, sh, _ := ScreenSize()
	switch runtime.GOOS {
	case "linux":
		return injectLinux(evt, sw, sh)
	case "windows":
		return injectWindows(evt, sw, sh)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func injectLinux(evt InputEvent, sw, sh int) error {
	switch evt.Type {
	case "mousemove":
		x := int(evt.X * float64(sw))
		y := int(evt.Y * float64(sh))
		return exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y)).Run()

	case "click":
		x := int(evt.X * float64(sw))
		y := int(evt.Y * float64(sh))
		button := "1"
		if evt.Button == 2 {
			button = "3"
		}
		return exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y), "click", button).Run()

	case "dblclick":
		x := int(evt.X * float64(sw))
		y := int(evt.Y * float64(sh))
		return exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y), "click", "--repeat", "2", "1").Run()

	case "contextmenu":
		x := int(evt.X * float64(sw))
		y := int(evt.Y * float64(sh))
		return exec.Command("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y), "click", "3").Run()

	case "keydown":
		xkey := mapKeyToXdotool(evt)
		if xkey == "" {
			return nil
		}
		return exec.Command("xdotool", "key", xkey).Run()

	default:
		return nil
	}
}

func injectWindows(evt InputEvent, sw, sh int) error {
	switch evt.Type {
	case "mousemove":
		x := int(evt.X * float64(sw))
		y := int(evt.Y * float64(sh))
		return exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d)", x, y)).Run()
	case "click":
		x := int(evt.X * float64(sw))
		y := int(evt.Y * float64(sh))
		script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d); `+
			`Add-Type -MemberDefinition '[DllImport("user32.dll")] public static extern void mouse_event(int f,int x,int y,int d,int e);' -Name U -Namespace W; `+
			`[W.U]::mouse_event(2,0,0,0,0); [W.U]::mouse_event(4,0,0,0,0)`, x, y)
		return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
	case "keydown":
		key := evt.Key
		if len(key) == 1 {
			script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('%s')`, escapeSendKeys(key))
			return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
		}
		mapped := mapKeyToSendKeys(evt)
		if mapped != "" {
			script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('%s')`, mapped)
			return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
		}
	}
	return nil
}

func escapeSendKeys(s string) string {
	r := strings.NewReplacer("+", "{+}", "^", "{^}", "%", "{%}", "~", "{~}", "(", "{(}", ")", "{)}", "{", "{{}", "}", "{}}")
	return r.Replace(s)
}

func mapKeyToSendKeys(evt InputEvent) string {
	switch evt.Key {
	case "Enter":
		return "{ENTER}"
	case "Backspace":
		return "{BACKSPACE}"
	case "Tab":
		return "{TAB}"
	case "Escape":
		return "{ESC}"
	case "ArrowUp":
		return "{UP}"
	case "ArrowDown":
		return "{DOWN}"
	case "ArrowLeft":
		return "{LEFT}"
	case "ArrowRight":
		return "{RIGHT}"
	case "Delete":
		return "{DELETE}"
	case "Home":
		return "{HOME}"
	case "End":
		return "{END}"
	case " ":
		return " "
	}
	if strings.HasPrefix(evt.Key, "F") && len(evt.Key) <= 3 {
		return "{" + evt.Key + "}"
	}
	return ""
}

func mapKeyToXdotool(evt InputEvent) string {
	key := evt.Key
	switch key {
	case "Enter":
		return "Return"
	case "Backspace":
		return "BackSpace"
	case "Tab":
		return "Tab"
	case "Escape":
		return "Escape"
	case "ArrowUp":
		return "Up"
	case "ArrowDown":
		return "Down"
	case "ArrowLeft":
		return "Left"
	case "ArrowRight":
		return "Right"
	case "Delete":
		return "Delete"
	case "Home":
		return "Home"
	case "End":
		return "End"
	case "PageUp":
		return "Prior"
	case "PageDown":
		return "Next"
	case " ":
		return "space"
	case "Control":
		return ""
	case "Shift":
		return ""
	case "Alt":
		return ""
	case "Meta":
		return ""
	}

	if len(key) == 1 {
		mods := ""
		if evt.Ctrl {
			mods += "ctrl+"
		}
		if evt.Alt {
			mods += "alt+"
		}
		if evt.Shift {
			mods += "shift+"
		}
		return mods + strings.ToLower(key)
	}

	// F-keys
	if strings.HasPrefix(key, "F") && len(key) <= 3 {
		return key
	}

	return key
}

// ParseInputEvent parses a JSON input event from the DataChannel.
func ParseInputEvent(data []byte) (InputEvent, error) {
	var evt InputEvent
	err := json.Unmarshal(data, &evt)
	return evt, err
}
