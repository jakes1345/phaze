package ui

import (
	"fmt"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type LiveStream struct {
	Host  string
	Title string
}

type LiveViewProps struct {
	Streams   []LiveStream
	WebBase   string
	OnRefresh func()
}

func NewLiveView(props LiveViewProps) fyne.CanvasObject {
	headerLabel := widget.NewLabelWithStyle("Live Streams", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	subtitleLabel := widget.NewLabel("Watch or broadcast live. Broadcasting opens in your browser for full camera+mic support.")
	subtitleLabel.Wrapping = fyne.TextWrapWord

	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), props.OnRefresh)

	goLiveBtn := widget.NewButtonWithIcon("Go Live in Browser", theme.MediaRecordIcon(), func() {
		u, _ := url.Parse(props.WebBase + "/web/")
		if u != nil {
			_ = fyne.CurrentApp().OpenURL(u)
		}
	})
	goLiveBtn.Importance = widget.HighImportance

	header := container.NewVBox(
		headerLabel,
		subtitleLabel,
		container.NewHBox(goLiveBtn, layout.NewSpacer(), refreshBtn),
		widget.NewSeparator(),
	)

	streamList := container.NewVBox()
	if len(props.Streams) == 0 {
		noStreams := widget.NewLabel("Nobody is live right now. Be the first!")
		noStreams.Alignment = fyne.TextAlignCenter
		streamList.Add(noStreams)
	} else {
		for _, s := range props.Streams {
			stream := s
			cardBg := canvas.NewRectangle(PhazePanel)
			cardBg.StrokeColor = PhazeSeparator
			cardBg.StrokeWidth = 1
			cardBg.CornerRadius = 10

			hostLabel := widget.NewLabelWithStyle(stream.Host, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			titleLabel := widget.NewLabel(stream.Title)
			titleLabel.Wrapping = fyne.TextWrapWord

			liveIndicator := canvas.NewText("● LIVE", PhazeBrand)
			liveIndicator.TextStyle = fyne.TextStyle{Bold: true}
			liveIndicator.TextSize = 12

			watchBtn := widget.NewButtonWithIcon("Watch", theme.MediaPlayIcon(), func() {
				u, _ := url.Parse(fmt.Sprintf("%s/web/#live/%s", props.WebBase, url.PathEscape(stream.Host)))
				if u != nil {
					_ = fyne.CurrentApp().OpenURL(u)
				}
			})

			info := container.NewVBox(
				container.NewHBox(hostLabel, layout.NewSpacer(), liveIndicator),
				titleLabel,
			)
			card := container.NewStack(
				cardBg,
				container.NewPadded(container.NewBorder(nil, nil, nil, watchBtn, info)),
			)
			streamList.Add(card)
		}
	}

	return container.NewBorder(
		container.NewPadded(header),
		nil, nil, nil,
		container.NewVScroll(container.NewPadded(streamList)),
	)
}
