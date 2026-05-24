package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// Story mirrors the server's Story struct.
type Story struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	MediaURL  string `json:"media_url"`
	MediaKind string `json:"media_kind"`
	Caption   string `json:"caption,omitempty"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	Views     int    `json:"views,omitempty"`
}

// NewStoriesView returns a Fyne widget that fetches and displays active
// stories from the Phaze server. apiBase is e.g. "https://phazechat.world".
// sessionToken is the Bearer token for auth.
func NewStoriesView(apiBase, sessionToken string) fyne.CanvasObject {
	statusLabel := widget.NewLabel("Loading stories...")
	list := container.NewVBox(statusLabel)
	scroll := container.NewVScroll(list)

	refreshBtn := widget.NewButton("Refresh", nil)

	doLoad := func() {
		go func() {
			stories, err := fetchStories(apiBase, sessionToken)
			fyne.Do(func() {
				list.RemoveAll()
				if err != nil {
					list.Add(widget.NewLabel("Failed to load stories: " + err.Error()))
					return
				}
				if len(stories) == 0 {
					list.Add(widget.NewLabel("No active stories right now."))
					return
				}
				// Group by author
				seen := map[string]bool{}
				var authors []string
				byAuthor := map[string][]Story{}
				for _, st := range stories {
					if !seen[st.Author] {
						seen[st.Author] = true
						authors = append(authors, st.Author)
					}
					byAuthor[st.Author] = append(byAuthor[st.Author], st)
				}
				for _, author := range authors {
					list.Add(storyAuthorCard(author, byAuthor[author]))
					list.Add(widget.NewSeparator())
				}
			})
		}()
	}

	refreshBtn.OnTapped = doLoad
	doLoad()

	header := container.NewBorder(nil, nil, nil, refreshBtn,
		widget.NewLabelWithStyle("Stories", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	return container.NewBorder(
		container.NewPadded(header),
		nil, nil, nil,
		scroll,
	)
}

func storyAuthorCard(author string, stories []Story) fyne.CanvasObject {
	authorLbl := widget.NewLabelWithStyle(author, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	countLbl := widget.NewLabel(fmt.Sprintf("%d story", len(stories)))
	if len(stories) != 1 {
		countLbl.SetText(fmt.Sprintf("%d stories", len(stories)))
	}

	var items []fyne.CanvasObject
	for _, st := range stories {
		st := st
		// Parse expiry for time-remaining display
		var expiresIn string
		if t, err := time.Parse(time.RFC3339, st.ExpiresAt); err == nil {
			rem := time.Until(t)
			if rem > 0 {
				h := int(rem.Hours())
				m := int(rem.Minutes()) % 60
				expiresIn = fmt.Sprintf("expires in %dh%dm", h, m)
			}
		}

		caption := st.Caption
		if caption == "" {
			caption = "[" + st.MediaKind + "]"
		}
		line := caption
		if expiresIn != "" {
			line += "  •  " + expiresIn
		}

		kindIcon := "🖼"
		if st.MediaKind == "video" {
			kindIcon = "🎬"
		}
		lbl := widget.NewLabel(kindIcon + " " + line)
		lbl.Wrapping = fyne.TextWrapWord
		items = append(items, lbl)
	}

	bg := canvas.NewRectangle(PhazePanel)
	bg.CornerRadius = 10

	card := container.NewVBox(
		container.NewHBox(authorLbl, layout.NewSpacer(), countLbl),
		container.NewVBox(items...),
	)
	return container.NewStack(bg, container.NewPadded(card))
}

func fetchStories(apiBase, sessionToken string) ([]Story, error) {
	req, err := http.NewRequest("GET", apiBase+"/api/v1/stories", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server %d: %s", resp.StatusCode, string(body))
	}
	var stories []Story
	if err := json.NewDecoder(resp.Body).Decode(&stories); err != nil {
		log.Printf("[stories] decode: %v", err)
		return nil, err
	}
	return stories, nil
}
