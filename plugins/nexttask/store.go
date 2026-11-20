package nexttask

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/jakeobsen/gt/kit"
)

// The workflow and the saved-for-later list live in the plugin's storage, so
// names, IDs and team conventions stay on this machine. Same keys and shapes
// as the Raycast extension's LocalStorage.
const (
	workflowKey = "workflow"
	savedKey    = "savedForLater"
)

func loadWorkflow(s kit.Storage) string {
	v, _ := s.Get(workflowKey)
	if strings.TrimSpace(v) == "" {
		return ""
	}
	return v
}

// saveWorkflow stores text; an empty text removes it, so the example is used.
func saveWorkflow(s kit.Storage, text string) error {
	if strings.TrimSpace(text) == "" {
		return s.Delete(workflowKey)
	}
	return s.Set(workflowKey, text)
}

// savedItem is something kept for later, exactly as it was when saved.
type savedItem struct {
	// ID is short and random; it is also how Claude refers to the item.
	ID    string `json:"id"`
	Title string `json:"title"`
	// URL is "" for things without a link, like a note.
	URL string `json:"url"`
	// Why is Claude's reason, for you to read later. It is never sent back
	// to Claude, which re-checks the item live anyway.
	Why     string `json:"why,omitempty"`
	SavedAt int64  `json:"savedAt"` // Unix milliseconds
}

func loadSaved(s kit.Storage) []savedItem {
	raw, ok := s.Get(savedKey)
	if !ok {
		return nil
	}
	var items []savedItem
	if json.Unmarshal([]byte(raw), &items) != nil {
		return nil
	}
	return items
}

func storeSaved(s kit.Storage, items []savedItem) error {
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return s.Set(savedKey, string(data))
}

// findSaved finds the saved entry for a link (or, for things without one,
// an identical title).
func findSaved(items []savedItem, url, title string) *savedItem {
	for i, it := range items {
		if (url != "" && it.URL == url) || (url == "" && it.URL == "" && it.Title == title) {
			return &items[i]
		}
	}
	return nil
}

// saveForLater keeps something as it is. Saving the same link (or link-less
// title) again changes nothing.
func saveForLater(s kit.Storage, title, url, why string, now time.Time) error {
	items := loadSaved(s)
	if findSaved(items, url, title) != nil {
		return nil
	}
	id := make([]byte, 4)
	_, _ = rand.Read(id)
	items = append(items, savedItem{ID: hex.EncodeToString(id), Title: title, URL: url, Why: why, SavedAt: now.UnixMilli()})
	return storeSaved(s, items)
}

func letGo(s kit.Storage, id string) error {
	items := loadSaved(s)
	kept := items[:0]
	for _, it := range items {
		if it.ID != id {
			kept = append(kept, it)
		}
	}
	return storeSaved(s, kept)
}
