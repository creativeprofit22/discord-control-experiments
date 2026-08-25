package jobs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"html"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

type feedDocument struct {
	XMLName xml.Name
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
	Entries []atomEntry `xml:"entry"`
}
type rssItem struct {
	GUID        string `xml:"guid"`
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	Published   string `xml:"pubDate"`
}
type atomEntry struct {
	ID        string `xml:"id"`
	Title     string `xml:"title"`
	Summary   string `xml:"summary"`
	Content   string `xml:"content"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Links     []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
}

func parseRSSAtom(data []byte, limit int) ([]application.SourceItem, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	var feed feedDocument
	if err := decoder.Decode(&feed); err != nil {
		return nil, errors.New("malformed RSS or Atom feed")
	}
	items := make([]application.SourceItem, 0, limit)
	switch strings.ToLower(feed.XMLName.Local) {
	case "rss", "rdf":
		for _, item := range feed.Channel.Items {
			if len(items) == limit {
				break
			}
			published := parseFeedTime(item.Published)
			link := canonicalSourceLink(item.Link)
			title, text := RenderSourceText(item.Title, 300), RenderSourceText(item.Description, 2000)
			id := stableSourceID(item.GUID, link, title, published)
			items = append(items, application.SourceItem{ID: id, Kind: "rss", Title: title, Text: text, Link: link, Published: published})
		}
	case "feed":
		for _, entry := range feed.Entries {
			if len(items) == limit {
				break
			}
			rawLink := ""
			for _, link := range entry.Links {
				if link.Rel == "" || link.Rel == "alternate" {
					rawLink = link.Href
					break
				}
			}
			published := parseFeedTime(entry.Published)
			if published.IsZero() {
				published = parseFeedTime(entry.Updated)
			}
			link := canonicalSourceLink(rawLink)
			title, text := RenderSourceText(entry.Title, 300), RenderSourceText(entry.Summary+" "+entry.Content, 2000)
			id := stableSourceID(entry.ID, link, title, published)
			items = append(items, application.SourceItem{ID: id, Kind: "atom", Title: title, Text: text, Link: link, Published: published})
		}
	default:
		return nil, errors.New("unsupported feed root")
	}
	if len(items) == 0 {
		return nil, errors.New("feed contains no bounded items")
	}
	return items, nil
}

func RenderSourceText(value string, maximum int) string {
	value = html.UnescapeString(stripMarkup(value))
	var output strings.Builder
	space := false
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			space = output.Len() > 0
			continue
		}
		if space {
			output.WriteByte(' ')
			space = false
		}
		if r == '@' {
			output.WriteRune('@')
			output.WriteRune('\u200b')
		} else {
			output.WriteRune(r)
		}
		if output.Len() >= maximum {
			break
		}
	}
	result := strings.TrimSpace(output.String())
	for len(result) > maximum {
		_, size := utf8.DecodeLastRuneInString(result)
		result = result[:len(result)-size]
	}
	return result
}

func stripMarkup(value string) string {
	var output strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			if inTag {
				inTag = false
				output.WriteByte(' ')
			} else {
				output.WriteRune(r)
			}
		default:
			if !inTag {
				output.WriteRune(r)
			}
		}
	}
	return output.String()
}
func canonicalSourceLink(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}
func stableSourceID(explicit, link, title string, published time.Time) string {
	explicit = RenderSourceText(explicit, 512)
	if explicit != "" {
		digest := sha256.Sum256([]byte(explicit))
		return hex.EncodeToString(digest[:])
	}
	digest := sha256.Sum256([]byte(link + "\x00" + title + "\x00" + published.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(digest[:])
}
func parseFeedTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
