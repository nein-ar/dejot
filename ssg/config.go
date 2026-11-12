// Package ssg turns Djot sources, data files and theme templates into a static
// site. It deliberately owns no vocabulary about any particular kind of site:
// pages, sections, data, templates and outputs are the only concepts it knows.
package ssg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the site configuration. Everything that is not a core path or
// identity setting belongs in Params; templates read it through .Site.Params.
type Config struct {
	Title        string   `toml:"title"`
	Description  string   `toml:"description"`
	BaseURL      string   `toml:"base_url"`
	LanguageCode string   `toml:"language_code"`
	Theme        string   `toml:"theme"`
	ThemesDir    string   `toml:"themes_dir"`
	LayoutsDir   string   `toml:"layouts_dir"`
	ContentDir   string   `toml:"content_dir"`
	DataDir      string   `toml:"data_dir"`
	StaticDir    string   `toml:"static_dir"`
	StaticDirs   []string `toml:"static_dirs"`
	OutputDir    string   `toml:"output_dir"`
	CacheDir     string   `toml:"cache_dir"`
	UglyURLs     bool     `toml:"ugly_urls"`
	Extensions   []string `toml:"extensions"`
	IndexFile    string   `toml:"index_file"`

	AllowCommands         bool     `toml:"allow_commands"`
	CommandTimeoutSeconds int      `toml:"command_timeout_seconds"`
	CommandDeps           []string `toml:"command_deps"`

	// RootDir is the directory commands run in and the base for relative
	// file/dir/git dependencies. It is derived, not configured.
	RootDir string `toml:"-"`

	Params map[string]any `toml:"params"`
	Menus  map[string]any `toml:"menus"`

	// Extra holds top level keys that are not core settings. They are merged
	// into Params so a site can put shared values at the top level if it wants.
	Extra map[string]any `toml:"-"`
}

var configKeys = map[string]bool{
	"title": true, "description": true, "base_url": true, "language_code": true,
	"theme": true, "themes_dir": true, "layouts_dir": true, "content_dir": true,
	"data_dir": true, "static_dir": true, "static_dirs": true, "output_dir": true, "cache_dir": true,
	"ugly_urls": true, "extensions": true, "index_file": true, "params": true,
	"menus": true, "allow_commands": true, "command_timeout_seconds": true,
	"command_deps": true,
}

// DefaultConfig returns a usable configuration. Paths are relative to the
// current directory, which is also the directory containing the config file
// once LoadConfig has resolved it.
func DefaultConfig() *Config {
	return &Config{
		Title:                 "Site",
		LanguageCode:          "en",
		Theme:                 "default",
		ThemesDir:             "themes",
		LayoutsDir:            "layouts",
		ContentDir:            "content",
		DataDir:               "data",
		StaticDir:             "static",
		OutputDir:             "public",
		CacheDir:              ".cache",
		Extensions:            []string{".djot"},
		IndexFile:             "_index",
		CommandTimeoutSeconds: 60,
		Params:                make(map[string]any),
		Menus:                 make(map[string]any),
		Extra:                 make(map[string]any),
	}
}

// LoadConfig reads a TOML file. Relative paths in the file are resolved
// relative to the file's directory, not the process working directory.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}

	if cfg.Params == nil {
		cfg.Params = make(map[string]any)
	}
	if cfg.Menus == nil {
		cfg.Menus = make(map[string]any)
	}
	if cfg.Extra == nil {
		cfg.Extra = make(map[string]any)
	}
	for k, v := range raw {
		if !configKeys[k] {
			cfg.Extra[k] = v
		}
	}

	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	cfg.RootDir = base
	cfg.resolvePaths(base)
	cfg.normalize()
	return cfg, nil
}

// NewConfig builds a configuration for programmatic use. The working
// directory is the base for every relative path.
func NewConfig() *Config {
	cfg := DefaultConfig()
	if cwd, err := os.Getwd(); err == nil {
		cfg.RootDir = cwd
	}
	cfg.normalize()
	return cfg
}

func (c *Config) resolvePaths(base string) {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) || isRemote(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	c.ThemesDir = resolve(c.ThemesDir)
	c.LayoutsDir = resolve(c.LayoutsDir)
	c.ContentDir = resolve(c.ContentDir)
	c.DataDir = resolve(c.DataDir)
	c.StaticDir = resolve(c.StaticDir)
	for i, p := range c.StaticDirs {
		c.StaticDirs[i] = resolve(p)
	}
	c.OutputDir = resolve(c.OutputDir)
	c.CacheDir = resolve(c.CacheDir)
}

func (c *Config) normalize() {
	if c.Params == nil {
		c.Params = make(map[string]any)
	}
	if c.Menus == nil {
		c.Menus = make(map[string]any)
	}
	for k, v := range c.Extra {
		if _, exists := c.Params[k]; !exists {
			c.Params[k] = v
		}
	}
	if c.LanguageCode == "" {
		c.LanguageCode = "en"
	}
	if c.Theme == "" {
		c.Theme = "default"
	}
	if c.IndexFile == "" {
		c.IndexFile = "_index"
	}
	if len(c.Extensions) == 0 {
		c.Extensions = []string{".djot"}
	}
	for i, ext := range c.Extensions {
		if ext != "" && ext[0] != '.' {
			c.Extensions[i] = "." + ext
		}
	}
	if c.BaseURL != "" && !strings.HasSuffix(c.BaseURL, "/") {
		c.BaseURL += "/"
	}
}

// ThemeDir returns the selected theme directory, or "" when it does not exist.
func (c *Config) ThemeDir() string {
	if c.Theme == "" || c.ThemesDir == "" {
		return ""
	}
	p := filepath.Join(c.ThemesDir, c.Theme)
	if stat, err := os.Stat(p); err == nil && stat.IsDir() {
		return p
	}
	return ""
}

type themeManifest struct {
	Name    string `toml:"name"`
	Extends string `toml:"extends"`
}

// ThemeChain returns the theme directories from the root ancestor to the
// selected theme. A theme may extend another one to inherit its layouts and
// static files; the child simply overrides files with the same name.
func (c *Config) ThemeChain() []string {
	var chain []string
	seen := make(map[string]bool)
	name := c.Theme
	for name != "" && !seen[name] {
		seen[name] = true
		dir := filepath.Join(c.ThemesDir, name)
		if stat, err := os.Stat(dir); err != nil || !stat.IsDir() {
			break
		}
		chain = append([]string{dir}, chain...)
		next := ""
		if data, err := os.ReadFile(filepath.Join(dir, "theme.toml")); err == nil {
			var m themeManifest
			if toml.Unmarshal(data, &m) == nil {
				next = m.Extends
			}
		}
		name = next
	}
	return chain
}

// Param returns a configuration parameter.
func (c *Config) Param(key string) any {
	if v, ok := c.Params[key]; ok {
		return v
	}
	return nil
}

func isRemote(p string) bool {
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "ftp://")
}
