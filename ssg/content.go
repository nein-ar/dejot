package ssg

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nein-ar/dejot/aspec"
	"github.com/nein-ar/dejot/params"
	"github.com/nein-ar/dejot/parser"
	"github.com/nein-ar/dejot/render/html"
)

var headingRE = regexp.MustCompile(`(?m)^#{1,6}[ \t]+(.+?)[ \t]*#*[ \t]*$`)

// New creates an empty site. Nothing is read from disk until Load is called.
func New(cfg *Config) *Site {
	if cfg == nil {
		cfg = NewConfig()
	}
	return &Site{
		Config:    cfg,
		Data:      make(map[string]any),
		Menus:     cfg.Menus,
		execCache: newCommandCache(filepath.Join(cfg.CacheDir, "exec")),
		depHashes: make(map[string]string),
	}
}

// AddData exposes a value to templates as .Site.Data.<name>. Programmatic data
// is merged with data loaded from disk; the programmatic value wins.
func (s *Site) AddData(name string, value any) *Site {
	s.Data[name] = value
	return s
}

// AddPage adds a page that did not come from the content directory. The page's
// Path and URL are normalised the same way disk pages are.
func (s *Site) AddPage(p *Page) *Site {
	if p == nil {
		return s
	}
	if p.Params == nil {
		p.Params = make(map[string]any)
	}
	p.synthetic = true
	if p.Kind == "" {
		p.Kind = "page"
	}
	if p.Path == "" {
		p.Path = pagePathFromRel(p.RelPath, p.Params, s.Config)
	}
	if p.URL == "" {
		p.URL = urlFromPath(p.Path)
	}
	if p.RelURL == "" {
		p.RelURL = s.relURL(p.URL)
	}
	if p.Content == "" && len(p.Raw) > 0 {
		// Deferred until the build is known to be necessary.
	}
	s.Pages = append(s.Pages, p)
	return s
}

// NewPage is a convenience constructor for programmatic pages.
func (s *Site) NewPage(title, relPath, layout string, p Params) *Page {
	pge := newPage()
	pge.Title = title
	pge.RelPath = relPath
	pge.Layout = layout
	pge.Kind = "page"
	for k, v := range p {
		pge.Params[k] = v
	}
	return pge
}

// Params is the small map used by programmatic page helpers.
type Params map[string]any

// Load reads content and data from the configured directories. Missing
// directories are not an error; a site may be assembled entirely in code.
func (s *Site) Load() error {
	if err := s.loadContent(); err != nil {
		return err
	}
	if err := s.loadData(); err != nil {
		return err
	}
	return nil
}

func (s *Site) loadContent() error {
	dir := s.Config.ContentDir
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}

	var pages []*Page
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if p != dir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") || !s.hasExtension(info.Name()) {
			return nil
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		pge, err := s.loadPage(p, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		if pge.Draft {
			return nil
		}
		pages = append(pages, pge)
		return nil
	})
	if err != nil {
		return err
	}

	s.Pages = append(s.Pages, pages...)
	s.buildTree()
	return nil
}

func (s *Site) hasExtension(name string) bool {
	for _, ext := range s.Config.Extensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func (s *Site) loadPage(filename, rel string) (*Page, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	fm, body, err := parseFrontMatter(data, filename)
	if err != nil {
		return nil, err
	}
	title, layout, slug, kind, weight, draft, date, updated, pageParams := normalizeFrontMatter(fm)

	ext := filepath.Ext(rel)
	relNoExt := strings.TrimSuffix(rel, ext)
	base := path.Base(relNoExt)
	dir := path.Dir(relNoExt)
	if dir == "." {
		dir = ""
	}

	isIndex := base == s.Config.IndexFile
	if kind == "" {
		if isIndex {
			if dir == "" {
				kind = "home"
			} else {
				kind = "section"
			}
		} else {
			kind = "page"
		}
	}

	relPath := ""
	if isIndex {
		relPath = dir
	} else {
		name := base
		if slug != "" {
			name = slug
		}
		relPath = path.Join(dir, name)
	}

	p := newPage()
	p.Source = filename
	p.RelPath = rel
	p.Title = title
	if p.Title == "" {
		p.Title = fallbackTitle(body, base)
	}
	if kind == "home" {
		p.Title = firstNonEmpty(title, s.Config.Title)
	}
	p.Kind = kind
	p.Layout = layout
	p.Weight = weight
	p.Draft = draft
	p.Date = date
	p.Updated = updated
	p.Params = pageParams
	p.Raw = body

	if rawURL := stringValue(fm["url"]); rawURL != "" {
		p.Path, p.URL, _ = normalizeURL(rawURL, s.Config.UglyURLs)
	} else {
		p.Path = pathFromRel(relPath, s.Config)
		p.URL = urlFromPath(p.Path)
	}
	p.RelURL = s.relURL(p.URL)

	return p, nil
}

func fallbackTitle(body []byte, base string) string {
	if m := headingRE.FindSubmatch(body); len(m) > 1 {
		return strings.TrimSpace(string(m[1]))
	}
	return base
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func pathFromRel(rel string, cfg *Config) string {
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return "index.html"
	}
	if cfg.UglyURLs {
		return rel + ".html"
	}
	return path.Join(rel, "index.html")
}

func pagePathFromRel(rel string, p map[string]any, cfg *Config) string {
	if raw := stringValue(p["url"]); raw != "" {
		out, _, _ := normalizeURL(raw, cfg.UglyURLs)
		return out
	}
	return pathFromRel(rel, cfg)
}

func urlFromPath(p string) string {
	if p == "index.html" {
		return "/"
	}
	if strings.HasSuffix(p, "/index.html") {
		return "/" + strings.TrimSuffix(p, "index.html")
	}
	return "/" + p
}

func (s *Site) relURL(u string) string {
	base := strings.TrimSuffix(s.Config.BaseURL, "/")
	if base == "" || u == "" {
		return u
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return base + "/" + strings.TrimPrefix(u, "/")
}

func (s *Site) buildTree() {
	for _, p := range s.Pages {
		p.Parent = nil
		p.Section = nil
		p.Children = nil
		p.Pages = nil
	}
	pages := make([]*Page, 0, len(s.Pages))
	seen := make(map[string]bool, len(s.Pages))
	for _, p := range s.Pages {
		if p.Path == "" {
			p.Path = pathFromRel(p.RelPath, s.Config)
		}
		if p.URL == "" {
			p.URL = urlFromPath(p.Path)
		}
		if p.RelURL == "" {
			p.RelURL = s.relURL(p.URL)
		}
		if seen[p.Path] {
			continue
		}
		seen[p.Path] = true
		pages = append(pages, p)
	}
	s.Pages = pages

	byPath := make(map[string]*Page, len(pages))
	var home *Page
	for _, p := range pages {
		byPath[p.Path] = p
		if p.Kind == "home" && home == nil {
			home = p
		}
	}
	if home == nil {
		// A site is not required to have a home page. If one is wanted it
		// should be content/_index.djot or an explicit programmatic page.
		s.Home = nil
	} else {
		s.Home = home
	}

	for _, p := range pages {
		if p == home {
			continue
		}
		// The nearest ancestor page. For pretty URLs the ancestor of
		// posts/hello/index.html is posts/index.html, then index.html.
		parentPath := p.Path
		for {
			dir := path.Dir(parentPath)
			if path.Base(parentPath) == "index.html" {
				dir = path.Dir(dir)
			}
			if dir == "." || dir == "/" || dir == "" {
				parentPath = "index.html"
			} else {
				parentPath = path.Join(dir, "index.html")
			}
			if parentPath == p.Path {
				parentPath = "index.html"
			}
			if parent, ok := byPath[parentPath]; ok {
				p.Parent = parent
				parent.addChild(p)
				if parent.isSection() {
					p.Section = parent
				} else if parent.Section != nil {
					p.Section = parent.Section
				}
				break
			}
			if parentPath == "index.html" {
				if home != nil {
					p.Parent = home
					home.addChild(p)
				}
				break
			}
		}
	}

	// A page whose path is a prefix of another page is structurally a
	// container even when its front matter did not declare one.
	for _, p := range pages {
		if len(p.Children) > 0 && p.Kind == "page" {
			p.Kind = "section"
		}
	}
	for _, p := range pages {
		sortPages(p.Pages)
		sortPages(p.Children)
	}
	sortPages(s.Pages)
}

func renderDjot(body []byte, baseDir string) (string, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return "", nil
	}
	expanded, _, err := aspec.ExpandSource(body, baseDir)
	if err != nil {
		return "", err
	}
	doc := parser.Parse(expanded)
	var buf bytes.Buffer
	if err := html.NewHTMLRenderer(doc, params.Params{}).Render(&buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Render converts Djot source to HTML. It is the library entry point for
// callers that do not need a whole site.
func Render(source []byte, baseDir string) (string, error) {
	return renderDjot(source, baseDir)
}

// RenderFile renders a Djot file, honouring aspec includes and variables.
func RenderFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	_, body, err := parseFrontMatter(data, filename)
	if err != nil {
		return "", err
	}
	return renderDjot(body, filepath.Dir(filename))
}

// Load is a shorthand for a site that only loads files from disk.
func Load(cfg *Config) (*Site, error) {
	s := New(cfg)
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

// String makes diagnostics readable.
func (p *Page) String() string {
	return fmt.Sprintf("%s (%s)", p.URL, p.Kind)
}
