package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// mentionHighlightColor is the accent color for @mention text.
var mentionHighlightColor = color.NRGBA{R: 99, G: 102, B: 241, A: 255} // indigo

func parseRichText(text string, slicer *AeroSlicer) []fyne.CanvasObject {
	var objects []fyne.CanvasObject
	pos := 0
	matches := emoticonRegex.FindAllStringIndex(text, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		if start > pos {
			objects = append(objects, parseMentions(text[pos:start])...)
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
		objects = append(objects, parseMentions(text[pos:])...)
	}
	if len(objects) == 0 {
		objects = append(objects, parseMentions(text)...)
	}
	return objects
}

// parseMentions splits plain text on @word tokens and renders them with
// a highlight colour so they stand out visually.
func parseMentions(text string) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	words := strings.Fields(text)
	// Rebuild word-by-word preserving spacing is complex; use simple field split.
	for i, w := range words {
		_ = i
		if strings.HasPrefix(w, "@") && len(w) > 1 {
			lbl := canvas.NewText(w, mentionHighlightColor)
			lbl.TextStyle = fyne.TextStyle{Bold: true}
			out = append(out, lbl)
		} else {
			out = append(out, widget.NewLabel(w))
		}
	}
	if len(out) == 0 {
		out = append(out, widget.NewLabel(text))
	}
	return out
}

// MessageBubbleOpts holds optional data for a message bubble.
type MessageBubbleOpts struct {
	// Reactions is a map of emoji -> list of usernames who reacted.
	Reactions map[string][]string
	// Kind is "voice", "file", or "text" (default).
	Kind string
	// OnReact is called when the user picks a quick reaction.
	OnReact func(emoji string)
	// OnEdit is called when the user selects Edit (own messages).
	OnEdit func()
	// OnDelete is called when the user selects Delete (own messages).
	OnDelete func()
}

func NewMessageBubble(author, text string, isMe bool, slicer *AeroSlicer) fyne.CanvasObject {
	return NewMessageBubbleEx(author, text, isMe, slicer, MessageBubbleOpts{})
}

func NewMessageBubbleEx(author, text string, isMe bool, slicer *AeroSlicer, opts MessageBubbleOpts) fyne.CanvasObject {
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

	displayText := text
	if opts.Kind == "voice" {
		displayText = "🎤 " + text
	} else if opts.Kind == "file" {
		displayText = "📎 " + text
	}

	bodyContent := container.NewHBox(parseRichText(displayText, slicer)...)

	// Reaction strip
	var reactionObjs []fyne.CanvasObject
	for emoji, users := range opts.Reactions {
		pill := widget.NewLabel(emoji + " " + strings.Join(users, ", "))
		reactionObjs = append(reactionObjs, pill)
	}

	contentItems := []fyne.CanvasObject{nameLabel, bodyContent}
	if len(reactionObjs) > 0 {
		contentItems = append(contentItems, container.NewHBox(reactionObjs...))
	}

	// Context menu buttons (shown inline as small text buttons for simplicity)
	if opts.OnReact != nil || opts.OnEdit != nil || opts.OnDelete != nil {
		var menuItems []fyne.CanvasObject
		quickReactions := []string{"👍", "❤️", "😂", "😮", "😢"}
		if opts.OnReact != nil {
			for _, r := range quickReactions {
				r := r
				btn := widget.NewButton(r, func() { opts.OnReact(r) })
				btn.Importance = widget.LowImportance
				menuItems = append(menuItems, btn)
			}
		}
		if isMe {
			if opts.OnEdit != nil {
				editBtn := widget.NewButton("✏️", opts.OnEdit)
				editBtn.Importance = widget.LowImportance
				menuItems = append(menuItems, editBtn)
			}
			if opts.OnDelete != nil {
				delBtn := widget.NewButton("🗑️", opts.OnDelete)
				delBtn.Importance = widget.DangerImportance
				menuItems = append(menuItems, delBtn)
			}
		}
		if len(menuItems) > 0 {
			contentItems = append(contentItems, container.NewHBox(menuItems...))
		}
	}

	content := container.NewVBox(contentItems...)
	return container.NewStack(rect, container.NewPadded(content))
}
