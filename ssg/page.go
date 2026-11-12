package ssg

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// Page is one rendered document. Content pages are loaded from disk; pages may
// also be added programmatically, which is what data driven generators do.
type Page struct {
	// Identity
	Title   string
	Kind    string // home, section or page
	Layout  string
	Path    string // output path without a leading slash, e.g. posts/hello/index.html
	URL     string // root relative URL, e.g. /posts/hello/
	RelURL  string // URL relative to Site.BaseURL
	RelPath string // source path relative to the content directory

	// Metadata
	Date    time.Time
	Updated time.Time
	Weight  int
	Draft   bool
	Params  map[string]any

	// Content
	Raw     []byte
	Content string // rendered HTML

	// Tree
	Parent   *Page
	Section  *Page
	Pages    []*Page
	Children []*Page

	// Source is the absolute source file, when the page came from disk.
	Source string

	synthetic bool
}

// Site is the in-memory representation used while building.
type Site struct {
	Config *Config
	Pages  []*Page
	Home   *Page
	Data   map[string]any
	Menus  map[string]any

	outputs   []outputSpec
	ts        *templateSet
	execCache *commandCache

	depMu     sync.Mutex
	depHashes map[string]string
}

func newPage() *Page {
	return &Page{Params: make(map[string]any)}
}

func (p *Page) addChild(c *Page) {
	p.Children = append(p.Children, c)
	p.Pages = append(p.Pages, c)
}

// isSection reports whether the page is a container. Sections and the home
// page are containers; ordinary pages are leaves.
func (p *Page) isSection() bool {
	return p.Kind == "home" || p.Kind == "section"
}

// sortPages gives a deterministic order: weight first, then newest, then title.
func sortPages(pages []*Page) {
	sort.SliceStable(pages, func(i, j int) bool {
		a, b := pages[i], pages[j]
		if a.Weight != b.Weight {
			return a.Weight < b.Weight
		}
		if !a.Date.Equal(b.Date) {
			return a.Date.After(b.Date)
		}
		return a.Title < b.Title
	})
}

func parseFrontMatter(data []byte, source string) (map[string]any, []byte, error) {
	fm := make(map[string]any)
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	// A delimiter only counts when it is the first line.
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return fm, data, nil
	}
	first := strings.TrimSpace(string(data[:nl]))
	if first != "---" && first != "+++" && first != ";;;" {
		return fm, data, nil
	}

	rest := data[nl+1:]
	offset := 0
	for offset <= len(rest) {
		end := bytes.IndexByte(rest[offset:], '\n')
		var line string
		var next int
		if end < 0 {
			line = strings.TrimSpace(string(rest[offset:]))
			next = len(rest)
		} else {
			line = strings.TrimSpace(string(rest[offset : offset+end]))
			next = offset + end + 1
		}
		if line == first {
			body := rest[next:]
			// A single leading newline is part of the front matter block,
			// not part of the document.
			body = bytes.TrimPrefix(body, []byte("\n"))
			if err := decodeFrontMatter(rest[:offset], first, fm); err != nil {
				return nil, nil, fmt.Errorf("%s: front matter: %w", source, err)
			}
			return fm, body, nil
		}
		if end < 0 {
			break
		}
		offset = next
	}

	return nil, nil, fmt.Errorf("%s: opening front matter delimiter without a closing one", source)
}

func decodeFrontMatter(data []byte, delimiter string, fm map[string]any) error {
	// +++ is explicitly TOML. --- is YAML by convention, but TOML is accepted
	// too because a site may prefer one language everywhere.
	if delimiter == "+++" {
		return toml.Unmarshal(data, &fm)
	}
	tomlErr := toml.Unmarshal(data, &fm)
	if tomlErr == nil {
		return nil
	}
	var yamlValue map[string]any
	if yamlErr := yaml.Unmarshal(data, &yamlValue); yamlErr == nil {
		for k, v := range yamlValue {
			fm[k] = v
		}
		return nil
	}
	return tomlErr
}

func normalizeFrontMatter(fm map[string]any) (string, string, string, string, int, bool, time.Time, time.Time, map[string]any) {
	params := make(map[string]any, len(fm))
	for k, v := range fm {
		params[k] = v
	}

	title := stringValue(fm["title"])
	layout := stringValue(fm["layout"])
	slug := stringValue(fm["slug"])
	kind := stringValue(fm["kind"])
	weight := intValue(fm["weight"])
	draft := boolValue(fm["draft"])
	date := timeValue(fm["date"])
	updated := timeValue(fm["updated"])
	if updated.IsZero() {
		updated = timeValue(fm["lastmod"])
	}
	if updated.IsZero() {
		updated = date
	}
	return title, layout, slug, kind, weight, draft, date, updated, params
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func intValue(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case uint64:
		return int(x)
	default:
		return 0
	}
}

func boolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "yes" || x == "1"
	default:
		return false
	}
}

func timeValue(v any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case time.Duration:
		return time.Time{}
	case int64:
		if x > 0 {
			return time.Unix(x, 0)
		}
	case int:
		if x > 0 {
			return time.Unix(int64(x), 0)
		}
	case string:
		if x == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, x); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// normalizeURL turns a front matter url into an output path and URL.
func normalizeURL(raw string, ugly bool) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	raw = strings.SplitN(raw, "#", 2)[0]
	raw = strings.SplitN(raw, "?", 2)[0]
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" {
		return "", "", false
	}
	if strings.HasSuffix(raw, "/") {
		raw += "index.html"
	} else if !strings.HasSuffix(raw, ".html") {
		if ugly {
			raw += ".html"
		} else {
			raw = path.Join(raw, "index.html")
		}
	}
	u := "/" + strings.TrimSuffix(raw, "index.html")
	return raw, u, true
}

// Convenience accessors for templates. They keep theme code short without
// putting presentation state on the site builder.

func (s *Site) Title() string       { return s.Config.Title }
func (s *Site) Description() string { return s.Config.Description }
func (s *Site) BaseURL() string     { return s.Config.BaseURL }
func (s *Site) LanguageCode() string {
	return s.Config.LanguageCode
}
func (s *Site) Params() map[string]any { return s.Config.Params }
