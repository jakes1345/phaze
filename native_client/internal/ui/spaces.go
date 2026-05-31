package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type SpaceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
	Visibility  string `json:"visibility"`
	Role        string `json:"role"`
}

type ChannelInfo struct {
	ID       string `json:"id"`
	ServerID string `json:"server_id"`
	Name     string `json:"name"`
	Topic    string `json:"topic"`
	Kind     string `json:"kind"`
}

type ChannelMsg struct {
	ID        int64               `json:"id"`
	ChannelID string              `json:"channel_id"`
	Sender    string              `json:"sender"`
	Body      string              `json:"body"`
	CreatedAt string              `json:"created_at"`
	Edited    bool                `json:"edited"`
	Deleted   bool                `json:"deleted"`
	Pinned    bool                `json:"pinned"`
	Reactions map[string][]string `json:"reactions,omitempty"`
}

type SpacesProps struct {
	Spaces         []SpaceInfo
	Channels       map[string][]ChannelInfo
	ActiveSpace    string
	ActiveChannel  string
	OnSelectSpace  func(id string)
	OnSelectChannel func(id string)
	OnJoinSpace    func(code string)
	OnCreateSpace  func(name, visibility string)
}

func NewSpacesView(props SpacesProps) fyne.CanvasObject {
	if len(props.Spaces) == 0 {
		return container.NewCenter(widget.NewLabel("You have not joined any Spaces yet.\nUse the web or mobile app to discover Spaces!"))
	}

	spaceList := widget.NewList(
		func() int { return len(props.Spaces) },
		func() fyne.CanvasObject {
			return widget.NewLabel("Space Name")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(props.Spaces[i].Name)
		},
	)
	spaceList.OnSelected = func(id widget.ListItemID) {
		if props.OnSelectSpace != nil {
			props.OnSelectSpace(props.Spaces[id].ID)
		}
	}

	left := container.NewBorder(widget.NewLabelWithStyle("Spaces", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}), nil, nil, nil, spaceList)

	var right fyne.CanvasObject
	if props.ActiveSpace != "" {
		chList := props.Channels[props.ActiveSpace]
		if len(chList) == 0 {
			right = container.NewCenter(widget.NewLabel("No channels in this Space"))
		} else {
			channelList := widget.NewList(
				func() int { return len(chList) },
				func() fyne.CanvasObject {
					return widget.NewLabel("# channel")
				},
				func(i widget.ListItemID, o fyne.CanvasObject) {
					o.(*widget.Label).SetText("# " + chList[i].Name)
				},
			)
			channelList.OnSelected = func(id widget.ListItemID) {
				if props.OnSelectChannel != nil {
					props.OnSelectChannel(chList[id].ID)
				}
			}
			right = container.NewBorder(widget.NewLabelWithStyle("Channels", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}), nil, nil, nil, channelList)
		}
	} else {
		right = container.NewCenter(widget.NewLabel("Select a Space"))
	}

	split := container.NewHSplit(left, right)
	split.Offset = 0.3
	return split
}
