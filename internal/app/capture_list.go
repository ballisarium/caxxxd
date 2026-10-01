package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ballisarium/caxxxd/internal/browser"
	"github.com/ballisarium/caxxxd/internal/ui"
)

type LiveChoice struct {
	Key string
	Choice
}
type livePrompter interface {
	ChooseLive(string, func() ([]LiveChoice, error), string) (string, error)
}

type capturedGroup struct {
	key   string
	items []browser.Candidate
}

func captureGroups(items []browser.Candidate) []capturedGroup {
	groups := []capturedGroup{}
	positions := map[string]int{}
	for _, item := range items {
		key := item.Group
		if key == "" {
			key = fmt.Sprintf("media:%d", item.ID)
		}
		index, found := positions[key]
		if !found {
			index = len(groups)
			positions[key] = index
			groups = append(groups, capturedGroup{key: key})
		}
		groups[index].items = append(groups[index].items, item)
	}
	return groups
}

func captureDetail(item browser.Candidate) string {
	parts := []string{}
	if item.Height > 0 {
		parts = append(parts, strconv.Itoa(item.Height)+"p")
	}
	if item.Duration > 0 {
		parts = append(parts, ui.FormatDuration(item.Duration))
	}
	if item.Codecs != "" {
		parts = append(parts, item.Codecs)
	}
	if item.Size > 0 {
		parts = append(parts, ui.FormatSize(item.Size, item.Approximate))
	} else {
		parts = append(parts, "size unknown")
	}
	if item.Source != "" {
		parts = append(parts, item.Source)
	} else {
		parts = append(parts, item.Host)
	}
	return strings.Join(parts, " · ")
}

func (a *App) chooseCapture(ctx context.Context) (int, error) {
	var groups []capturedGroup
	choices := func() ([]LiveChoice, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := a.capture.Err(); err != nil {
			return nil, err
		}
		groups = captureGroups(a.capture.Candidates())
		rows := make([]LiveChoice, 0, len(groups)+2)
		for i, group := range groups {
			item := group.items[0]
			label := fmt.Sprintf("%d · %s", i+1, item.Kind)
			if len(group.items) > 1 {
				label += fmt.Sprintf(" · %d streams", len(group.items))
			}
			rows = append(rows, LiveChoice{Key: group.key, Choice: Choice{Label: label, Detail: captureDetail(item)}})
		}
		rows = append(rows, LiveChoice{Key: "refresh", Choice: Choice{Label: "Refresh streams", Detail: "include new browser requests"}}, LiveChoice{Key: "back", Choice: backChoice})
		return rows, nil
	}
	var key string
	if live, ok := a.prompt.(livePrompter); ok {
		var err error
		key, err = live.ChooseLive("Captured media", choices, a.captureCursor)
		if err != nil {
			return -1, err
		}
	} else {
		rows, err := choices()
		if err != nil {
			return -1, err
		}
		static := make([]Choice, len(rows))
		for i := range rows {
			static[i] = rows[i].Choice
		}
		picked, err := a.prompt.Choose("Captured media", static, 0)
		if err != nil {
			return -1, err
		}
		key = rows[picked].Key
	}
	a.captureCursor = key
	if key == "refresh" {
		return -1, nil
	}
	if key == "back" {
		return -2, nil
	}
	for _, group := range groups {
		if group.key != key {
			continue
		}
		if len(group.items) == 1 {
			return group.items[0].ID, nil
		}
		rows := make([]Choice, len(group.items)+1)
		for i, item := range group.items {
			rows[i] = Choice{Label: fmt.Sprintf("%d · %s", i+1, item.Kind), Detail: captureDetail(item)}
		}
		rows[len(group.items)] = backChoice
		picked, err := a.prompt.Choose("Streams for this video", rows, 0)
		if err != nil {
			return -1, err
		}
		if picked == len(group.items) {
			return -1, nil
		}
		return group.items[picked].ID, nil
	}
	return -1, nil
}
