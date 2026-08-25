package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

type githubRelease struct {
	ID         int64     `json:"id"`
	Tag        string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	URL        string    `json:"html_url"`
	Published  time.Time `json:"published_at"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
}

func parseGitHubReleases(data []byte, limit int) ([]application.SourceItem, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var releases []githubRelease
	if err := decoder.Decode(&releases); err != nil {
		return nil, errors.New("malformed GitHub release response")
	}
	if len(releases) > 100 {
		return nil, errors.New("GitHub release response exceeds item limit")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("GitHub release response contains trailing data")
	}
	items := make([]application.SourceItem, 0, limit)
	for _, release := range releases {
		if len(items) == limit {
			break
		}
		if release.Draft || release.ID < 1 {
			continue
		}
		link := canonicalSourceLink(release.URL)
		if link == "" {
			return nil, errors.New("GitHub release contains invalid canonical URL")
		}
		title := RenderSourceText(release.Name, 300)
		if title == "" {
			title = RenderSourceText(release.Tag, 300)
		}
		items = append(items, application.SourceItem{ID: stableSourceID(fmt.Sprint(release.ID), link, title, release.Published), Kind: "github_release", Title: title, Text: RenderSourceText(release.Body, 2000), Link: link, Published: release.Published.UTC()})
	}
	return items, nil
}
