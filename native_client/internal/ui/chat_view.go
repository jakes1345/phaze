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
	OnBack        func()
	Contacts      []string
}

type ChatView struct {
	Container *fyne.Container
	Pulsar    *PhazePulsar
}

func NewChatView(props ChatViewProps) *ChatView {
	if IsMobile() {
		return newMobileChatView(props)
	}
	return newDesktopChatView(props)
}

func newDesktopChatView(props ChatViewProps) *ChatView {
	pulsar := NewPhazePulsar()
	icon := canvas.NewCircle(color.NRGBA{G: 200, B: 0, A: 255})
	if props.Slicer != nil {
		icon = canvas.NewCircle(color.Transparent)
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

	msgPlaceholder := widget.NewLabel("No messages yet.")

	input := widget.NewMultiLineEntry()
	input.SetPlaceHolder("Type a message here...")

	emojiBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
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

	input.OnChanged = buildMentionHandler(input, props)

	leftActions := container.NewHBox(emojiBtn, fileBtn, micBtn)
	inputArea := container.NewBorder(nil, nil, leftActions, sendBtn, container.NewPadded(input))

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

func newMobileChatView(props ChatViewProps) *ChatView {
	pulsar := NewPhazePulsar()

	backBtn := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		if props.OnBack != nil {
			props.OnBack()
		}
	})
	backBtn.Importance = widget.LowImportance

	nameLabel := widget.NewLabelWithStyle(props.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	statusLabel := widget.NewLabelWithStyle(props.Status, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})

	callBtn := widget.NewButtonWithIcon("", theme.ConfirmIcon(), props.OnCall)
	callBtn.Importance = widget.LowImportance

	moreBtn := widget.NewButtonWithIcon("", theme.MoreVerticalIcon(), func() {})
	moreBtn.Importance = widget.LowImportance

	header := container.NewBorder(
		nil, nil,
		backBtn,
		container.NewHBox(callBtn, moreBtn),
		container.NewVBox(nameLabel, statusLabel),
	)
	headerBg := canvas.NewRectangle(PhazePanel)
	headerContainer := container.NewStack(headerBg, container.NewPadded(header))

	msgPlaceholder := widget.NewLabel("No messages yet.")

	input := widget.NewEntry()
	input.SetPlaceHolder("Message...")

	micBtn := widget.NewButtonWithIcon("", theme.MediaRecordIcon(), func() {
		if props.OnVoiceRecord != nil {
			props.OnVoiceRecord()
		}
	})

	fileBtn := widget.NewButtonWithIcon("", theme.FileIcon(), props.OnSendFile)

	sendBtn := widget.NewButtonWithIcon("", theme.MailSendIcon(), func() {
		if input.Text != "" {
			props.OnSend(input.Text)
			input.SetText("")
		}
	})

	input.OnSubmitted = func(text string) {
		if text != "" {
			props.OnSend(text)
			input.SetText("")
		}
	}
	input.OnChanged = func(s string) {
		if s != "" && props.OnTyping != nil {
			props.OnTyping()
		}
	}

	inputRow := container.NewBorder(nil, nil, container.NewHBox(fileBtn, micBtn), sendBtn, input)

	main := container.NewBorder(
		headerContainer,
		container.NewVBox(widget.NewSeparator(), container.NewPadded(inputRow)),
		nil, nil,
		container.NewPadded(msgPlaceholder),
	)
	return &ChatView{Container: main, Pulsar: pulsar}
}

func buildMentionHandler(input *widget.Entry, props ChatViewProps) func(string) {
	var mentionPopup *widget.PopUp
	return func(s string) {
		if s != "" && props.OnTyping != nil {
			props.OnTyping()
		}
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
}
