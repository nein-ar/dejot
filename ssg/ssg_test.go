package ssg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func testSite(t *testing.T) (*Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg := NewConfig()
	cfg.RootDir = root
	cfg.Title = "Test"
	cfg.BaseURL = "https://example.test/"
	cfg.ContentDir = filepath.Join(root, "content")
	cfg.DataDir = filepath.Join(root, "data")
	cfg.LayoutsDir = filepath.Join(root, "layouts")
	cfg.StaticDir = filepath.Join(root, "static")
	cfg.OutputDir = filepath.Join(root, "public")
	cfg.CacheDir = filepath.Join(root, "cache")
	cfg.ThemesDir = filepath.Join(root, "themes")
	return cfg, root
}

func TestBuildContentAndURLs(t *testing.T) {
	cfg, _ := testSite(t)
	writeFile(t, filepath.Join(cfg.ContentDir, "_index.djot"), "---\ntitle: Home\n---\nWelcome *home*.\n")
	writeFile(t, filepath.Join(cfg.ContentDir, "posts", "one.djot"), "---\ntitle: One\ndate: 2026-01-02\n---\n# First\n\nBody text.\n")
	writeFile(t, filepath.Join(cfg.ContentDir, "posts", "_index.djot"), "---\ntitle: Posts\n---\n")
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "base.html"), `<html><body>{{ .Body }}</body></html>`)
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "home.html"), `<h1>{{ .Page.Title }}</h1>{{ .Content }}`)
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "list.html"), `<h2>{{ .Page.Title }}</h2>`)
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "single.html"), `<article><h1>{{ .Page.Title }}</h1>{{ .Content }}</article>`)
	writeFile(t, filepath.Join(cfg.StaticDir, "css", "site.css"), "body{}")

	site, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := site.Build()
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Pages != 3 {
		t.Fatalf("unexpected result: %+v", res)
	}

	home := read(t, filepath.Join(cfg.OutputDir, "index.html"))
	if !strings.Contains(home, "Welcome <strong>home</strong>") || !strings.Contains(home, "<html>") {
		t.Fatalf("bad home: %s", home)
	}
	page := read(t, filepath.Join(cfg.OutputDir, "posts", "one", "index.html"))
	if !strings.Contains(page, "<h1>One</h1>") || !strings.Contains(page, "Body text") {
		t.Fatalf("bad page: %s", page)
	}
	if read(t, filepath.Join(cfg.OutputDir, "css", "site.css")) != "body{}" {
		t.Fatal("static file missing")
	}

	res, err = site.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Fatal("second build should be a no-op")
	}

	// A removed source must remove its output.
	if err := os.Remove(filepath.Join(cfg.ContentDir, "posts", "one.djot")); err != nil {
		t.Fatal(err)
	}
	site, err = Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err = site.Build()
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed == 0 {
		t.Fatal("stale output was not removed")
	}
	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "posts", "one", "index.html")); !os.IsNotExist(err) {
		t.Fatal("stale file still exists")
	}
}

func TestProgrammaticPageAndData(t *testing.T) {
	cfg, _ := testSite(t)
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "single.html"), `{{ .Site.Data.facts.answer }} {{ .Page.Params.kind }} {{ .Content }}`)
	site := New(cfg)
	site.AddData("facts", map[string]any{"answer": 42})
	p := site.NewPage("Generated", "generated/thing", "", Params{"kind": "thing"})
	p.Raw = []byte("Hello *generated*.")
	site.AddPage(p)

	res, err := site.Build()
	if err != nil {
		t.Fatal(err)
	}
	if res.Pages != 1 {
		t.Fatalf("expected one generated page, got %d", res.Pages)
	}
	out := read(t, filepath.Join(cfg.OutputDir, "generated", "thing", "index.html"))
	if !strings.Contains(out, "42") || !strings.Contains(out, "thing") || !strings.Contains(out, "<strong>generated</strong>") {
		t.Fatalf("bad generated page: %s", out)
	}
}

func TestURLOverrideAndHref(t *testing.T) {
	cfg, _ := testSite(t)
	writeFile(t, filepath.Join(cfg.ContentDir, "files", "view.djot"), "---\ntitle: View\nurl: /files/view.html\n---\n")
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "single.html"), `<a href="{{ href .Page.Path "/index.html" }}">home</a>`)
	site, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	out := read(t, filepath.Join(cfg.OutputDir, "files", "view.html"))
	if !strings.Contains(out, `href="../index.html"`) {
		t.Fatalf("bad relative link: %s", out)
	}
}

func TestThemeSwitchRemovesOldOutputs(t *testing.T) {
	cfg, root := testSite(t)
	writeFile(t, filepath.Join(cfg.ContentDir, "page.djot"), "---\ntitle: Page\n---\n")
	writeFile(t, filepath.Join(cfg.ThemesDir, "one", "layouts", "_default", "single.html"), `one`)
	writeFile(t, filepath.Join(cfg.ThemesDir, "one", "static", "one.css"), `one`)
	writeFile(t, filepath.Join(cfg.ThemesDir, "two", "layouts", "_default", "single.html"), `two`)
	writeFile(t, filepath.Join(cfg.ThemesDir, "two", "static", "two.css"), `two`)

	cfg.Theme = "one"
	site, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(cfg.OutputDir, "page", "index.html")); got != "one" {
		t.Fatalf("theme one: %q", got)
	}

	cfg.Theme = "two"
	site, err = Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(cfg.OutputDir, "page", "index.html")); got != "two" {
		t.Fatalf("theme two: %q", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "one.css")); !os.IsNotExist(err) {
		t.Fatal("old theme static file was not removed")
	}
	_ = root
}

func TestTemplateHelpers(t *testing.T) {
	cfg, _ := testSite(t)
	writeFile(t, filepath.Join(cfg.ContentDir, "a.djot"), "---\ntitle: A\n---\n")
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "single.html"),
		`{{ $d := dict "x" 2 }}{{ index $d "x" }}|{{ default "fallback" "" }}|{{ formatBytes 2048 }}|{{ plural 2 "port" }}|{{ json .Page.Title }}`)
	site, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	out := read(t, filepath.Join(cfg.OutputDir, "a", "index.html"))
	for _, want := range []string{"2|fallback|2.0 KiB|2 ports|&#34;A&#34;"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestThemeInheritance(t *testing.T) {
	cfg, _ := testSite(t)
	writeFile(t, filepath.Join(cfg.ContentDir, "page.djot"), "---\ntitle = \"Page\"\n---\n")
	writeFile(t, filepath.Join(cfg.ThemesDir, "base", "theme.toml"), "name = \"base\"\n")
	writeFile(t, filepath.Join(cfg.ThemesDir, "base", "layouts", "_default", "single.html"), "base {{ asset \"assets/base.css\" }}")
	writeFile(t, filepath.Join(cfg.ThemesDir, "base", "static", "assets", "base.css"), "base")
	writeFile(t, filepath.Join(cfg.ThemesDir, "child", "theme.toml"), "name = \"child\"\nextends = \"base\"\n")
	writeFile(t, filepath.Join(cfg.ThemesDir, "child", "layouts", "_default", "single.html"), "child {{ asset \"assets/child.css\" }}")
	writeFile(t, filepath.Join(cfg.ThemesDir, "child", "static", "assets", "child.css"), "child")
	cfg.Theme = "child"

	site, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	out := read(t, filepath.Join(cfg.OutputDir, "page", "index.html"))
	if !strings.Contains(out, "child https://example.test/assets/child.css") {
		t.Fatalf("child layout was not used: %q", out)
	}
	read(t, filepath.Join(cfg.OutputDir, "assets", "base.css"))
	read(t, filepath.Join(cfg.OutputDir, "assets", "child.css"))
}
