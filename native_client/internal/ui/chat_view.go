package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type ChatViewProps struct {
	Name          string
	Status        string
	IsGroup       bool
	Slicer        *AeroSlicer
	OnCall        func()
	OnBlock       func()
	OnReport      func()
	OnSend        func(text string)
	OnSendFile    func()
	OnVoiceRecord func()
	OnTyping      func()
	// Contacts list for @mention dropdown
	Contacts []string
}

type ChatView struct {
	Container *fyne.Container
	Pulsar    *PhazePulsar
}

func NewChatView(props ChatViewProps) *ChatView {
	pulsar := NewPhazePulsar()
	// 1. Header
	icon := canvas.NewCircle(color.NRGBA{G: 200, B: 0, A: 255})
	if props.Slicer != nil {
		// Use the real status dot
		res := props.Slicer.GetStatusIcon(props.Status)
		iconImg := canvas.NewImageFromResource(res)
		iconImg.Resize(fyne.NewSize(12, 12))
		icon = canvas.NewCircle(color.Transparent) // Placeholder to keep logic simple for now
	}
	icon.Resize(fyne.NewSize(12, 12))
	
	nameLabel := widget.NewLabelWithStyle(props.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	statusLabel := widget.NewLabelWithStyle(props.Status, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	
	headerInfo := container.NewVBox(nameLabel, statusLabel)
	
	blockBtn := widget.NewButtonWithIcon("Block", theme.CancelIcon(), func() {
		if props.OnBlock != nil {
			props.OnBlock()
		}
	})
	blockBtn.Importance = widget.DangerImportance

	reportBtn := widget.NewButtonWithIcon("Report", theme.WarningIcon(), func() {
		if props.OnReport != nil {
			props.OnReport()
		}
	})
	reportBtn.Importance = widget.MediumImportance

	headerActions := container.NewHBox(
		widget.NewButtonWithIcon("Call", theme.ConfirmIcon(), props.OnCall),
		widget.NewButtonWithIcon("Video", theme.VisibilityIcon(), func() {}),
		blockBtn,
		reportBtn,
		widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {}),
	)
	
	header := container.NewBorder(nil, nil, icon, headerActions, headerInfo)
	headerBg := canvas.NewRectangle(PhazePanel)
	headerContainer := container.NewStack(headerBg, container.NewPadded(header))

	// 2. Message Area
	msgPlaceholder := widget.NewLabel("No messages yet.")

	// 3. Input Area
	input := widget.NewMultiLineEntry()
	input.SetPlaceHolder("Type a message here...")

	emojiBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		// Use the correct popup with slicer
		win := fyne.CurrentApp().Driver().AllWindows()[0]
		ShowEmoticonPopup(win.Canvas(), props.Slicer, fyne.NewPos(100, 300), func(s string) {
			input.SetText(input.Text + s)
		})
	})
	
	fileBtn := widget.NewButtonWithIcon("", theme.FileIcon(), props.OnSendFile)

	micBtn := widget.NewButtonWithIcon("", theme.MediaRecordIcon(), func() {
		if props.OnVoiceRecord != nil {
			props.OnVoiceRecord()
		}
	})

	sendBtn := widget.NewButtonWithIcon("", theme.MailSendIcon(), func() {
		if input.Text != "" {
			props.OnSend(input.Text)
			input.SetText("")
		}
	})

	// @mention autocomplete: show dropdown when user types @
	var mentionPopup *widget.PopUp
	input.OnChanged = func(s string) {
		if s != "" {
			props.OnTyping()
		}
		// Detect @ trigger
		if mentionPopup != nil {
			mentionPopup.Hide()
			mentionPopup = nil
		}
		if len(props.Contacts) == 0 {
			return
		}
		atIdx := strings.LastIndex(s, "@")
		if atIdx < 0 {
			return
		}
		query := strings.ToLower(s[atIdx+1:])
		if strings.ContainsAny(query, " \t\n") {
			return
		}
		var matches []string
		for _, c := range props.Contacts {
			if query == "" || strings.HasPrefix(strings.ToLower(c), query) {
				matches = append(matches, c)
				if len(matches) >= 5 {
					break
				}
			}
		}
		if len(matches) == 0 {
			return
		}
		items := make([]fyne.CanvasObject, len(matches))
		for i, m := range matches {
			m := m
			btn := widget.NewButton("@"+m, func() {
				// Replace the @query with the chosen mention
				newText := s[:atIdx+1] + m + " "
				input.SetText(newText)
				if mentionPopup != nil {
					mentionPopup.Hide()
					mentionPopup = nil
				}
			})
			items[i] = btn
		}
		win := fyne.CurrentApp().Driver().AllWindows()[0]
		mentionPopup = widget.NewPopUp(container.NewVBox(items...), win.Canvas())
		mentionPopup.ShowAtPosition(fyne.NewPos(100, 400))
	}

	leftActions := container.NewHBox(emojiBtn, fileBtn, micBtn)
	inputArea := container.NewBorder(nil, nil, leftActions, sendBtn, container.NewPadded(input))
	
	// Final Layout
	main := container.NewBorder(
		headerContainer,
		container.NewVBox(
			container.NewHBox(layout.NewSpacer(), pulsar.Container, layout.NewSpacer()),
			widget.NewSeparator(), 
			container.NewPadded(inputArea),
		),
		nil, nil,
		container.NewPadded(msgPlaceholder),
	)
	return &ChatView{Container: main, Pulsar: pulsar}
}
