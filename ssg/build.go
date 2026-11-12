package ssg

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BuildOptions controls a build.
type BuildOptions struct {
	// Force ignores the cached fingerprint and re-renders every page.
	Force bool
	// DryRun gathers what would change without touching the output directory.
	DryRun bool
}

// BuildResult describes what happened. A skipped build is the common case and
// costs one fingerprint, no Djot parsing and no template execution.
type BuildResult struct {
	Skipped     bool
	Fingerprint string
	Pages       int
	Written     int
	Removed     int
	Duration    time.Duration
}

type outputSpec struct {
	Path     string
	Data     []byte
	Template string
	Context  any
}

// AddOutput queues a raw output file, such as JSON or a generated script.
func (s *Site) AddOutput(p string, data []byte) *Site {
	s.outputs = append(s.outputs, outputSpec{Path: cleanOutputPath(p), Data: data})
	return s
}

// AddTemplateOutput queues an output rendered from a template. This is how
// feeds, machine indexes and other non-page resources stay themeable.
func (s *Site) AddTemplateOutput(p, templateName string, ctx any) *Site {
	s.outputs = append(s.outputs, outputSpec{Path: cleanOutputPath(p), Template: templateName, Context: ctx})
	return s
}

// Build renders the site. Pages already present in memory are included.
func (s *Site) Build() (BuildResult, error) {
	return s.BuildWithOptions(BuildOptions{})
}

// BuildWithOptions is Build with explicit control over cache and dry run.
func (s *Site) BuildWithOptions(opts BuildOptions) (BuildResult, error) {
	start := time.Now()
	result := BuildResult{}

	if s.Config == nil {
		return result, fmt.Errorf("ssg: nil config")
	}
	if s.Config.OutputDir == "" {
		return result, fmt.Errorf("ssg: empty output directory")
	}

	s.resetDependencyHashes()
	s.buildTree()
	sort.SliceStable(s.Pages, func(i, j int) bool { return s.Pages[i].Path < s.Pages[j].Path })

	m := loadManifest(manifestPath(s.Config))
	fp := s.fingerprint()
	result.Fingerprint = fp

	if !opts.Force && m.Fingerprint == fp && m.complete(s.Config.OutputDir) {
		result.Skipped = true
		result.Duration = time.Since(start)
		return result, nil
	}

	ts, err := s.templateSet()
	if err != nil {
		return result, err
	}

	// Djot work happens only after the cache has failed to prove a no-op.
	for _, p := range s.Pages {
		if len(p.Raw) > 0 && p.Content == "" {
			content, err := renderDjot(p.Raw, sourceDir(p))
			if err != nil {
				return result, fmt.Errorf("ssg: %s: %w", p.Source, err)
			}
			p.Content = content
		}
	}

	if opts.DryRun {
		result.Pages = len(s.Pages)
		result.Duration = time.Since(start)
		return result, nil
	}

	if err := os.MkdirAll(s.Config.OutputDir, 0755); err != nil {
		return result, err
	}

	touched := make(map[string]bool)
	next := newManifest()
	next.Fingerprint = fp

	write := func(rel string, data []byte) error {
		rel = cleanOutputPath(rel)
		if rel == "" {
			return fmt.Errorf("ssg: empty output path")
		}
		h := hashBytes(data)
		touched[rel] = true
		next.Entries[rel] = manifestEntry{Hash: h, Size: int64(len(data))}
		if old, ok := m.Entries[rel]; ok && old.Hash == h {
			if _, err := os.Stat(filepath.Join(s.Config.OutputDir, filepath.FromSlash(rel))); err == nil {
				return nil
			}
		}
		full := filepath.Join(s.Config.OutputDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return err
		}
		tmp := full + ".ssg-tmp"
		if err := os.WriteFile(tmp, data, 0644); err != nil {
			return err
		}
		if err := os.Rename(tmp, full); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		result.Written++
		return nil
	}

	if err := s.copyStatic(write, touched, next); err != nil {
		return result, err
	}

	for _, p := range s.Pages {
		out, err := s.renderPage(p, ts)
		if err != nil {
			return result, err
		}
		if err := write(p.Path, out); err != nil {
			return result, err
		}
		result.Pages++
	}

	for _, spec := range s.outputs {
		data := spec.Data
		if spec.Template != "" {
			name := ts.lookup(spec.Template)
			if name == "" {
				return result, fmt.Errorf("ssg: output template %q not found", spec.Template)
			}
			out, err := ts.execute(name, spec.Context)
			if err != nil {
				return result, err
			}
			data = out
		}
		if err := write(spec.Path, data); err != nil {
			return result, err
		}
	}

	// Remove outputs that this build no longer produces. This also makes a
	// theme switch clean: the previous theme's generated files do not linger.
	removedDirs := make(map[string]bool)
	for rel := range m.Entries {
		if touched[rel] {
			continue
		}
		full := filepath.Join(s.Config.OutputDir, filepath.FromSlash(rel))
		if err := os.Remove(full); err == nil {
			result.Removed++
			removedDirs[filepath.Dir(full)] = true
		}
	}
	pruneEmptyDirs(s.Config.OutputDir, removedDirs)

	if err := saveManifest(manifestPath(s.Config), next); err != nil {
		return result, err
	}
	if s.execCache != nil {
		if err := s.execCache.GC(); err != nil {
			return result, err
		}
	}
	result.Duration = time.Since(start)
	return result, nil
}

func sourceDir(p *Page) string {
	if p.Source != "" {
		return filepath.Dir(p.Source)
	}
	return ""
}

func cleanOutputPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(filepath.ToSlash(p), "/")
	p = path.Clean(p)
	if p == "." {
		return ""
	}
	if strings.HasPrefix(p, "../") || p == ".." {
		return ""
	}
	return p
}

func pruneEmptyDirs(root string, dirs map[string]bool) {
	for dir := range dirs {
		for strings.HasPrefix(dir, root) && dir != root {
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) > 0 {
				break
			}
			if err := os.Remove(dir); err != nil {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
}

func (s *Site) copyStatic(write func(string, []byte) error, touched map[string]bool, next *manifest) error {
	roots := make([]string, 0, 2)
	for _, theme := range s.Config.ThemeChain() {
		roots = append(roots, filepath.Join(theme, "static"))
	}
	if s.Config.StaticDir != "" {
		roots = append(roots, s.Config.StaticDir)
	}
	roots = append(roots, s.Config.StaticDirs...)
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return write(filepath.ToSlash(rel), data)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Site) fingerprint() string {
	h := sha256.New()
	add := func(format string, values ...any) {
		fmt.Fprintf(h, format, values...)
		h.Write([]byte{0})
	}

	add("version:%s", manifestVersion)
	cfgJSON, _ := json.Marshal(s.Config)
	add("config:%s", cfgJSON)
	dataJSON, _ := json.Marshal(s.Data)
	add("data:%s", dataJSON)
	menuJSON, _ := json.Marshal(s.Menus)
	add("menus:%s", menuJSON)

	for _, theme := range s.Config.ThemeChain() {
		hashTree(h, filepath.Join(theme, "layouts"))
		hashTree(h, filepath.Join(theme, "static"))
	}
	hashTree(h, s.Config.LayoutsDir)
	hashTree(h, s.Config.ContentDir)
	hashTree(h, s.Config.DataDir)
	hashTree(h, s.Config.StaticDir)
	for _, dir := range s.Config.StaticDirs {
		hashTree(h, dir)
	}

	depSpecs := append([]string{}, s.Config.CommandDeps...)
	depSpecs = append(depSpecs, s.templateCommandDeps()...)
	sort.Strings(depSpecs)
	lastDep := ""
	for _, spec := range depSpecs {
		if spec == lastDep {
			continue
		}
		lastDep = spec
		add("cmd-dep:%s:%s", spec, s.hashDependency(Dependency{Spec: spec}))
	}

	for _, p := range s.Pages {
		add("page:%s|%s|%s|%s|%d|%d|%t", p.Path, p.Kind, p.Layout, p.Title, p.Weight, p.Date.Unix(), p.Draft)
		params, _ := json.Marshal(p.Params)
		add("params:%s", params)
		add("raw:%s", p.Raw)
		if len(p.Raw) == 0 {
			add("content:%s", p.Content)
		}
	}
	for _, out := range s.outputs {
		add("out:%s|%s", out.Path, out.Template)
		if out.Template != "" {
			ctx, _ := json.Marshal(out.Context)
			add("ctx:%s", ctx)
		} else {
			add("bytes:%s", out.Data)
		}
	}
	return hexHash(h)
}

func hexHash(h hash.Hash) string {
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashTree(h hash.Hash, dir string) {
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}
	var files []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		files = append(files, p)
		return nil
	})
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "file:%s:%d:", filepath.ToSlash(rel), len(data))
		h.Write(data)
		h.Write([]byte{0})
	}
}

// ReadOutput is a small convenience for callers that need a rendered file
// without walking the output directory themselves.
func ReadOutput(cfg *Config, rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(cfg.OutputDir, filepath.FromSlash(cleanOutputPath(rel))))
}

// WriteTree writes a set of in-memory files. It is useful for tests and for
// generators that need to materialise their sources before a build.
func WriteTree(root string, files map[string][]byte) error {
	for rel, data := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(full, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// DetectChanges reports the fingerprint that a build would use. It is handy
// for watch loops that want to say "nothing changed" without rendering.
func (s *Site) DetectChanges() string {
	s.resetDependencyHashes()
	return s.fingerprint()
}
