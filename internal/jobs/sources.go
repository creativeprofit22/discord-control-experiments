package jobs

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

type SourceDefinition struct {
	Name, Kind, URL, GitHubOwner, GitHubRepo string
	MaxBodyBytes                             int64
}

type AddressResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}
type DialAddress func(context.Context, string, string) (net.Conn, error)

type sourceCursor struct {
	ETag     string   `json:"etag,omitempty"`
	Modified string   `json:"modified,omitempty"`
	Seen     []string `json:"seen,omitempty"`
}

type SourceConnector struct {
	definitions map[string]SourceDefinition
	client      *http.Client
}

func NewSourceConnector(definitions []SourceDefinition, resolver AddressResolver, dial DialAddress) (*SourceConnector, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dial == nil {
		d := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
		dial = d.DialContext
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 16, MaxIdleConnsPerHost: 4, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	transport.DialContext = constrainedDial(resolver, dial)
	connector := &SourceConnector{definitions: map[string]SourceDefinition{}, client: &http.Client{Transport: transport, Timeout: 20 * time.Second}}
	for _, definition := range definitions {
		if err := validateSourceDefinition(definition); err != nil {
			return nil, err
		}
		if _, exists := connector.definitions[definition.Name]; exists {
			return nil, errors.New("duplicate source definition")
		}
		connector.definitions[definition.Name] = definition
	}
	return connector, nil
}

func constrainedDial(resolver AddressResolver, dial DialAddress) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid source dial address")
		}
		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("source host resolution failed")
		}
		for _, candidate := range addresses {
			if !allowedSourceIP(candidate.IP) {
				return nil, errors.New("source host resolved to a blocked address")
			}
		}
		return dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
}

func allowedSourceIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return false
	}
	blocked := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32")}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validateSourceDefinition(definition SourceDefinition) error {
	if definition.Name == "" || len(definition.Name) > 64 || definition.MaxBodyBytes < 1024 || definition.MaxBodyBytes > 8<<20 {
		return errors.New("invalid source definition")
	}
	switch definition.Kind {
	case "rss":
		u, err := url.Parse(definition.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return errors.New("RSS source requires fixed HTTPS URL")
		}
	case "github_releases":
		if definition.URL != "" || !githubComponent(definition.GitHubOwner) || !githubComponent(definition.GitHubRepo) {
			return errors.New("GitHub source requires fixed owner and repository")
		}
	default:
		return errors.New("unknown source kind")
	}
	return nil
}

func (c *SourceConnector) Fetch(ctx context.Context, name, cursor string, limit int) (application.SourceBatch, error) {
	definition, ok := c.definitions[name]
	if !ok {
		return application.SourceBatch{}, errors.New("unknown configured source")
	}
	if limit < 1 || limit > 100 {
		return application.SourceBatch{}, errors.New("source item limit must be between 1 and 100")
	}
	state, err := decodeSourceCursor(cursor)
	if err != nil {
		return application.SourceBatch{}, err
	}
	endpoint := definition.URL
	if definition.Kind == "github_releases" {
		endpoint = "https://api.github.com/repos/" + url.PathEscape(definition.GitHubOwner) + "/" + url.PathEscape(definition.GitHubRepo) + "/releases?per_page=" + fmt.Sprint(limit)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return application.SourceBatch{}, err
	}
	request.Header.Set("Accept", sourceAccept(definition.Kind))
	request.Header.Set("User-Agent", "discord-control-automation/1")
	if state.ETag != "" {
		request.Header.Set("If-None-Match", state.ETag)
	}
	if state.Modified != "" {
		request.Header.Set("If-Modified-Since", state.Modified)
	}
	client := *c.client
	origin, _ := url.Parse(endpoint)
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if definition.Kind != "rss" || len(via) >= 3 || request.URL.Scheme != "https" || !strings.EqualFold(request.URL.Hostname(), origin.Hostname()) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return application.SourceBatch{}, fmt.Errorf("fetch configured source: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return application.SourceBatch{Cursor: cursor, NotModified: true}, nil
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return application.SourceBatch{}, fmt.Errorf("source returned status class %dxx", response.StatusCode/100)
	}
	if !allowedContentType(definition.Kind, response.Header.Get("Content-Type")) {
		return application.SourceBatch{}, errors.New("source returned disallowed content type")
	}
	body, err := readBoundedBody(response.Body, definition.MaxBodyBytes)
	if err != nil {
		return application.SourceBatch{}, err
	}
	var items []application.SourceItem
	if definition.Kind == "rss" {
		items, err = parseRSSAtom(body, limit)
	} else {
		items, err = parseGitHubReleases(body, limit)
	}
	if err != nil {
		return application.SourceBatch{}, err
	}
	seen := map[string]bool{}
	for _, id := range state.Seen {
		seen[id] = true
	}
	fresh := items[:0]
	for _, item := range items {
		if !seen[item.ID] {
			fresh = append(fresh, item)
			state.Seen = append(state.Seen, item.ID)
			seen[item.ID] = true
		}
	}
	if len(state.Seen) > 500 {
		state.Seen = state.Seen[len(state.Seen)-500:]
	}
	state.ETag, state.Modified = response.Header.Get("ETag"), response.Header.Get("Last-Modified")
	next, err := json.Marshal(state)
	if err != nil {
		return application.SourceBatch{}, err
	}
	return application.SourceBatch{Items: fresh, Cursor: string(next)}, nil
}

func decodeSourceCursor(cursor string) (sourceCursor, error) {
	var state sourceCursor
	if cursor == "" {
		return state, nil
	}
	if len(cursor) > 64<<10 {
		return state, errors.New("source cursor exceeds limit")
	}
	decoder := json.NewDecoder(strings.NewReader(cursor))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil || len(state.Seen) > 500 {
		return sourceCursor{}, errors.New("invalid source cursor")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return sourceCursor{}, errors.New("invalid source cursor")
	}
	return state, nil
}
func readBoundedBody(reader io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errors.New("source response exceeds size limit")
	}
	return data, nil
}
func sourceAccept(kind string) string {
	if kind == "rss" {
		return "application/rss+xml, application/atom+xml, application/xml, text/xml"
	}
	return "application/vnd.github+json"
}
func allowedContentType(kind, value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	if kind == "rss" {
		return value == "application/rss+xml" || value == "application/atom+xml" || value == "application/xml" || value == "text/xml"
	}
	return value == "application/json" || value == "application/vnd.github+json"
}
func githubComponent(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return value != "." && value != ".."
}
