package ssg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecCachesAndInvalidatesByDependency(t *testing.T) {
	cfg, root := testSite(t)
	cfg.AllowCommands = true
	depFile := filepath.Join(root, "dep.txt")
	if err := os.WriteFile(depFile, []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cfg.ContentDir, "page.djot"), "---\ntitle: Page\n---\n")
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "single.html"),
		`{{ exec "sh" "-c" "echo x >> counter; cat dep.txt" (dep "file:dep.txt") }}`)

	site, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	out := read(t, filepath.Join(cfg.OutputDir, "page", "index.html"))
	if !strings.Contains(out, "one") {
		t.Fatalf("bad first output: %q", out)
	}
	counter := read(t, filepath.Join(root, "counter"))
	if strings.Count(counter, "x") != 1 {
		t.Fatalf("command should run once, counter=%q", counter)
	}

	// A dependency change must both rebuild and re-run the command.
	if err := os.WriteFile(depFile, []byte("two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	out = read(t, filepath.Join(cfg.OutputDir, "page", "index.html"))
	if !strings.Contains(out, "two") {
		t.Fatalf("bad second output: %q", out)
	}
	counter = read(t, filepath.Join(root, "counter"))
	if strings.Count(counter, "x") != 2 {
		t.Fatalf("command should run twice, counter=%q", counter)
	}

	// An unrelated static change rebuilds templates but must not re-run a
	// command whose own dependencies did not change.
	writeFile(t, filepath.Join(cfg.StaticDir, "site.css"), "body{}")
	if _, err := site.Build(); err != nil {
		t.Fatal(err)
	}
	counter = read(t, filepath.Join(root, "counter"))
	if strings.Count(counter, "x") != 2 {
		t.Fatalf("command should have been cached, counter=%q", counter)
	}
}

func TestExecRequiresOptIn(t *testing.T) {
	cfg, _ := testSite(t)
	writeFile(t, filepath.Join(cfg.ContentDir, "page.djot"), "---\ntitle: Page\n---\n")
	writeFile(t, filepath.Join(cfg.LayoutsDir, "_default", "single.html"),
		`{{ exec "echo" "hello" }}`)
	site, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.Build(); err == nil {
		t.Fatal("expected command execution to be disabled")
	}
}
