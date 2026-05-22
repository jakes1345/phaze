package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func parseRichText(text string, slicer *AeroSlicer) []fyne.CanvasObject {
	var objects []fyne.CanvasObject
	pos := 0
	matches := emoticonRegex.FindAllStringIndex(text, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		if start > pos {
			objects = append(objects, widget.NewLabel(text[pos:start]))
		}
		tok := text[start:end]

		emoji := NewAnimatedEmoji(tok, slicer)
		if emoji != nil {
			emoji.imageObj.SetMinSize(fyne.NewSize(24, 24))
			objects = append(objects, emoji)
		} else {
			objects = append(objects, widget.NewLabel(tok))
		}
		pos = end
	}
	if pos < len(text) {
		objects = append(objects, widget.NewLabel(text[pos:]))
	}
	if len(objects) == 0 {
		objects = append(objects, widget.NewLabel(text))
	}
	return objects
}

func NewMessageBubble(author, text string, isMe bool, slicer *AeroSlicer) fyne.CanvasObject {
	// Flat, rounded bubble matching the web client's modern look.
	// Outgoing: solid indigo brand; incoming: subtle hover/panel surface.
	rect := canvas.NewRectangle(color.Transparent)
	rect.CornerRadius = 14
	rect.StrokeWidth = 0
	if isMe {
		rect.FillColor = PhazeBrand
	} else {
		rect.FillColor = PhazeBubbleIn
	}

	nameLabel := widget.NewLabelWithStyle(author, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	bodyContent := container.NewHBox(parseRichText(text, slicer)...)

	content := container.NewVBox(nameLabel, bodyContent)
	return container.NewStack(rect, container.NewPadded(content))
}
