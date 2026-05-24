package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/pion/mediadevices"
)

type SettingsProps struct {
	ServerAddr     string
	SoundEnabled   bool
	OnSave         func(server string, sound bool)
	OnAudioChange  func(deviceName string)
	OnLinkPhone    func(number string)
	OnVerifyPhone  func(number, code string)
	OnPurgeEmail   func()
	Sentinel       interface {
		GetDiagnosticSummaries() string
	}
}

func NewSettingsDialog(props SettingsProps) fyne.CanvasObject {
	// 1. General Tab
	serverEntry := widget.NewEntry()
	serverEntry.SetText(props.ServerAddr)
	soundCheck := widget.NewCheck("Enable Sound Effects", nil)
	soundCheck.SetChecked(props.SoundEnabled)
	
	generalTab := container.NewVBox(
		widget.NewLabelWithStyle("General Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewForm(
			widget.NewFormItem("Nexus Server", serverEntry),
		),
		soundCheck,
	)

	// 2. Audio & Video Tab
	audioDevices := []string{"System Default"}
	devices := mediadevices.EnumerateDevices()
	for _, d := range devices {
		if d.Kind == mediadevices.AudioInput {
			audioDevices = append(audioDevices, d.DeviceID) // Using DeviceID as name for now
		}
	}
	
	audioSelect := widget.NewSelect(audioDevices, props.OnAudioChange)
	audioSelect.SetSelected("System Default")

	avTab := container.NewVBox(
		widget.NewLabelWithStyle("Audio & Video Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewForm(
			widget.NewFormItem("Microphone", audioSelect),
		),
		widget.NewButtonWithIcon("Test Audio", theme.MediaPlayIcon(), func() {}),
	)

	// 3. Privacy Tab (P2P Mesh + Phone + Email purge)
	phoneEntry := widget.NewEntry()
	phoneEntry.SetPlaceHolder("+1 555 000 0000")
	var pendingPhone string
	linkPhoneBtn := widget.NewButtonWithIcon("Link Phone", theme.ConfirmIcon(), func() {
		if phoneEntry.Text == "" {
			return
		}
		pendingPhone = phoneEntry.Text
		if props.OnLinkPhone != nil {
			props.OnLinkPhone(pendingPhone)
		}
	})
	linkPhoneBtn.Importance = widget.HighImportance

	codeEntry := widget.NewEntry()
	codeEntry.SetPlaceHolder("6-digit SMS code")
	verifyPhoneBtn := widget.NewButtonWithIcon("Verify", theme.ConfirmIcon(), func() {
		if codeEntry.Text == "" || pendingPhone == "" {
			return
		}
		if props.OnVerifyPhone != nil {
			props.OnVerifyPhone(pendingPhone, codeEntry.Text)
		}
		codeEntry.SetText("")
	})

	purgeEmailBtn := widget.NewButtonWithIcon("Remove email from account", theme.DeleteIcon(), func() {
		if props.OnPurgeEmail != nil {
			props.OnPurgeEmail()
		}
	})
	purgeEmailBtn.Importance = widget.DangerImportance

	privacyTab := container.NewVBox(
		widget.NewLabelWithStyle("Sovereign Mesh Privacy", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewCheck("Announce on Public DHT", func(bool) {}),
		widget.NewCheck("Allow Local mDNS Discovery", func(bool) {}),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Phone Number", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Link a phone number to your account for account recovery."),
		widget.NewForm(widget.NewFormItem("Phone", phoneEntry)),
		linkPhoneBtn,
		widget.NewLabel("Enter the SMS verification code:"),
		widget.NewForm(widget.NewFormItem("Code", codeEntry)),
		verifyPhoneBtn,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Email Privacy", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Warning: removing your email disables password recovery via email."),
		purgeEmailBtn,
	)

	// 4. Sentinel Tab (Acting on peer audit)
	sentinelReport := widget.NewRichTextFromMarkdown("")
	sentinelReport.Hide()

	scanBtn := widget.NewButtonWithIcon("Run Forensic Network Scan", theme.SearchIcon(), func() {
		sentinelReport.ParseMarkdown(props.Sentinel.GetDiagnosticSummaries())
		sentinelReport.Show()
	})
	scanBtn.Importance = widget.WarningImportance

	sentinelTab := container.NewVScroll(container.NewVBox(
		widget.NewLabelWithStyle("Phaze Sentinel Diagnostic", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Autonomous system health and network path auditing."),
		widget.NewSeparator(),
		scanBtn,
		sentinelReport,
	))

	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("General", theme.SettingsIcon(), generalTab),
		container.NewTabItemWithIcon("Audio & Video", theme.MediaVideoIcon(), avTab),
		container.NewTabItemWithIcon("Privacy", theme.VisibilityIcon(), privacyTab),
		container.NewTabItemWithIcon("Sentinel", theme.InfoIcon(), sentinelTab),
	)
	
	saveBtn := widget.NewButtonWithIcon("Save", theme.ConfirmIcon(), func() {
		props.OnSave(serverEntry.Text, soundCheck.Checked)
	})
	saveBtn.Importance = widget.HighImportance

	return container.NewBorder(nil, container.NewPadded(saveBtn), nil, nil, tabs)
}
