package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type FriendInfo struct {
	Username    string
	Status      string
	Avatar      string
	Mood        string
	DisplayName string
	Supporter   bool
}

type SidebarProps struct {
	Username        string
	Status          string
	Mood            string
	AvatarPath      string
	Slicer          *AeroSlicer
	OnChatOpen      func(name string)
	OnChatWindow    func(name string)
	OnAddFriend     func()
	OnNewGroup      func()
	OnSearch        func(query string)
	OnSettings      func()
	OnProfile       func()
	OnDialCall      func(number string)
	PSTNDialEnabled bool
	OnStatusChange  func(status string)
	RecentChats     []FriendInfo
	CompactMode     bool
	SpacesView      fyne.CanvasObject
}

func NewPhazeSidebar(props SidebarProps) fyne.CanvasObject {
	if IsMobile() {
		return newMobileSidebar(props)
	}
	return newDesktopSidebar(props)
}

func newDesktopSidebar(props SidebarProps) fyne.CanvasObject {
	avatarSize := float32(48)
	if props.CompactMode {
		avatarSize = 32
	}
	avatar := NewAvatarWithStatus(avatarSize, props.Status, props.AvatarPath)
	nameLabel := widget.NewLabelWithStyle(props.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	avatarBtn := widget.NewButton("", props.OnProfile)
	avatarBtn.Importance = widget.LowImportance

	var rightContent fyne.CanvasObject
	if !props.CompactMode {
		statusSelect := widget.NewSelect([]string{"Online", "Away", "Do Not Disturb", "Invisible"}, props.OnStatusChange)
		statusSelect.SetSelected(props.Status)
		rightContent = container.NewVBox(nameLabel, statusSelect)
	} else {
		rightContent = container.NewVBox(nameLabel)
	}

	profileHeader := container.NewHBox(
		container.NewStack(container.NewPadded(avatar), avatarBtn),
		rightContent,
	)

	profileBg := canvas.NewRectangle(PhazeBlue)
	profileContainer := container.NewStack(profileBg, container.NewPadded(profileHeader))
	sidebarHeader := container.NewVBox(profileContainer)

	search := widget.NewEntry()
	search.SetPlaceHolder("Search...")

	actionButtons := container.NewGridWithColumns(3,
		widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), props.OnAddFriend),
		widget.NewButtonWithIcon("Group", theme.ContentAddIcon(), props.OnNewGroup),
		widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), props.OnSettings),
	)

	search.OnSubmitted = props.OnSearch

	list := widget.NewList(
		func() int { return len(props.RecentChats) },
		func() fyne.CanvasObject {
			size := float32(36)
			if props.CompactMode {
				size = 24
			}
			return container.NewHBox(
				container.NewMax(NewAvatarWithStatus(size, "Offline", "")),
				widget.NewLabel("Contact Name"),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			friend := props.RecentChats[i]
			label := o.(*fyne.Container).Objects[1].(*widget.Label)
			name := friend.Username
			if friend.Supporter {
				name += " 💜"
			}
			label.SetText(name)

			avatarWrap := o.(*fyne.Container).Objects[0].(*fyne.Container)
			size := float32(36)
			if props.CompactMode {
				size = 24
			}
			avatarWrap.Objects = []fyne.CanvasObject{NewAvatarWithStatus(size, friend.Status, friend.Avatar)}
			avatarWrap.Refresh()
		},
	)
	var lastID widget.ListItemID = -1
	var lastTime time.Time

	list.OnSelected = func(id widget.ListItemID) {
		now := time.Now()
		if id == lastID && now.Sub(lastTime) < 500*time.Millisecond {
			if props.OnChatWindow != nil {
				props.OnChatWindow(props.RecentChats[id].Username)
			}
		} else {
			props.OnChatOpen(props.RecentChats[id].Username)
		}
		lastID = id
		lastTime = now
		list.Unselect(id)
	}

	var dialTab *container.TabItem
	if props.PSTNDialEnabled && props.OnDialCall != nil {
		dialTab = container.NewTabItem("Dial", NewPhazeDialpad(DialpadProps{OnCall: props.OnDialCall}))
	} else {
		hint := widget.NewLabel("PSTN dialing is turned off.\n\nUse the phone or camera button inside a chat for Phaze-to-Phaze voice and video over WebRTC — no carrier or Twilio call charges.")
		hint.Wrapping = fyne.TextWrapWord
		dialTab = container.NewTabItem("Calls", container.NewPadded(hint))
	}
	contactsHint := widget.NewLabel("Your saved contacts appear under Recent after you chat. Use Search above to find someone on your Nexus server, or Add from the home view. A dedicated contact list is planned.")
	contactsHint.Wrapping = fyne.TextWrapWord
	tabs := container.NewAppTabs(
		container.NewTabItem("Spaces", props.SpacesView),
		container.NewTabItem("Recent", list),
		container.NewTabItem("Contacts", container.NewPadded(contactsHint)),
		dialTab,
	)

	sidebarContent := container.NewBorder(
		container.NewVBox(sidebarHeader, container.NewPadded(search), actionButtons, widget.NewSeparator()),
		nil, nil, nil,
		tabs,
	)

	bg := canvas.NewRectangle(PhazeShell)

	return container.NewStack(bg, container.NewPadded(sidebarContent))
}

// newMobileSidebar builds a full-screen contact list optimized for touch.
func newMobileSidebar(props SidebarProps) fyne.CanvasObject {
	avatar := NewAvatarWithStatus(40, props.Status, props.AvatarPath)
	nameLabel := widget.NewLabelWithStyle(props.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	avatarBtn := widget.NewButton("", props.OnProfile)
	avatarBtn.Importance = widget.LowImportance

	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), props.OnSettings)
	settingsBtn.Importance = widget.LowImportance

	profileRow := container.NewBorder(
		nil, nil,
		container.NewStack(container.NewPadded(avatar), avatarBtn),
		settingsBtn,
		nameLabel,
	)

	profileBg := canvas.NewRectangle(PhazeBlue)
	header := container.NewStack(profileBg, container.NewPadded(profileRow))

	search := widget.NewEntry()
	search.SetPlaceHolder("Search...")
	search.OnSubmitted = props.OnSearch

	actionRow := container.NewGridWithColumns(2,
		widget.NewButtonWithIcon("Add Friend", theme.ContentAddIcon(), props.OnAddFriend),
		widget.NewButtonWithIcon("New Group", theme.ContentAddIcon(), props.OnNewGroup),
	)

	list := widget.NewList(
		func() int { return len(props.RecentChats) },
		func() fyne.CanvasObject {
			return container.NewBorder(
				nil, nil,
				container.NewMax(NewAvatarWithStatus(44, "Offline", "")),
				nil,
				container.NewVBox(
					widget.NewLabel("Contact Name"),
					widget.NewLabelWithStyle("mood", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
				),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			friend := props.RecentChats[i]
			border := o.(*fyne.Container)
			avatarWrap := border.Objects[2].(*fyne.Container)
			avatarWrap.Objects = []fyne.CanvasObject{NewAvatarWithStatus(44, friend.Status, friend.Avatar)}
			avatarWrap.Refresh()

			infoBox := border.Objects[0].(*fyne.Container)
			infoBox.Objects[0].(*widget.Label).SetText(friend.Username)
			mood := friend.Mood
			if mood == "" {
				mood = friend.Status
			}
			infoBox.Objects[1].(*widget.Label).SetText(mood)
		},
	)

	list.OnSelected = func(id widget.ListItemID) {
		props.OnChatOpen(props.RecentChats[id].Username)
		list.Unselect(id)
	}

	var tabContent fyne.CanvasObject
	if props.SpacesView != nil {
		tabs := container.NewAppTabs(
			container.NewTabItem("Spaces", props.SpacesView),
			container.NewTabItem("Recent", list),
		)
		tabContent = tabs
	} else {
		tabContent = list
	}

	content := container.NewBorder(
		container.NewVBox(header, container.NewPadded(search), actionRow, widget.NewSeparator()),
		nil, nil, nil,
		tabContent,
	)

	bg := canvas.NewRectangle(PhazeShell)
	return container.NewStack(bg, content)
}
