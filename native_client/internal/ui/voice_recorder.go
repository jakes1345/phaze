package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type VoiceRecorder struct {
	Container *fyne.Container
	OnStop    func()
	OnCancel  func()

	timerLabel *widget.Label
	stopBtn    *widget.Button
	cancelBtn  *widget.Button
	ticker     *time.Ticker
	startTime  time.Time
	done       chan struct{}
}

func NewVoiceRecorder(onStop, onCancel func()) *VoiceRecorder {
	timerLabel := widget.NewLabelWithStyle("0:00", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	timerLabel.Importance = widget.HighImportance

	recDot := canvas.NewCircle(PhazeBrand)
	recDot.Resize(fyne.NewSize(12, 12))

	recLabel := widget.NewLabelWithStyle("Recording voice message…", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	stopBtn := widget.NewButtonWithIcon("Send", theme.MailSendIcon(), nil)
	stopBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), nil)
	cancelBtn.Importance = widget.DangerImportance

	vr := &VoiceRecorder{
		OnStop:     onStop,
		OnCancel:   onCancel,
		timerLabel: timerLabel,
		stopBtn:    stopBtn,
		cancelBtn:  cancelBtn,
		done:       make(chan struct{}),
	}

	stopBtn.OnTapped = func() {
		vr.Stop()
		if vr.OnStop != nil {
			vr.OnStop()
		}
	}
	cancelBtn.OnTapped = func() {
		vr.Stop()
		if vr.OnCancel != nil {
			vr.OnCancel()
		}
	}

	bg := canvas.NewRectangle(PhazeBrandSoft)
	bg.CornerRadius = 12

	row := container.NewHBox(cancelBtn, recLabel, timerLabel, stopBtn)
	vr.Container = container.NewStack(bg, container.NewPadded(row))
	return vr
}

func (vr *VoiceRecorder) Start() {
	vr.startTime = time.Now()
	vr.ticker = time.NewTicker(time.Second)
	go func() {
		for {
			select {
			case <-vr.done:
				return
			case <-vr.ticker.C:
				elapsed := time.Since(vr.startTime)
				mins := int(elapsed.Minutes())
				secs := int(elapsed.Seconds()) % 60
				text := fmt.Sprintf("%d:%02d", mins, secs)
				fyne.Do(func() {
					vr.timerLabel.SetText(text)
				})
			}
		}
	}()
}

func (vr *VoiceRecorder) Stop() {
	if vr.ticker != nil {
		vr.ticker.Stop()
	}
	select {
	case <-vr.done:
	default:
		close(vr.done)
	}
}
