package ssg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Dependency is a declared input to a command. It is produced by the dep
// template function and consumed by exec.
type Dependency struct {
	Spec string
}

func (d Dependency) String() string { return d.Spec }

var depPattern = regexp.MustCompile(`\bdep\s+"([^"]+)"`)

func (s *Site) dep(spec string) (Dependency, error) {
	kind, _, ok := strings.Cut(spec, ":")
	if !ok {
		return Dependency{}, fmt.Errorf("dependency %q needs a kind prefix (file:, dir:, data:, git:)", spec)
	}
	switch kind {
	case "file", "dir", "data", "git":
		return Dependency{Spec: spec}, nil
	default:
		return Dependency{}, fmt.Errorf("unknown dependency kind %q in %q", kind, spec)
	}
}

func (s *Site) execCommand(args ...any) (string, error) {
	if !s.Config.AllowCommands {
		return "", fmt.Errorf("command execution is disabled; set allow_commands = true")
	}

	var command []string
	var deps []Dependency
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			command = append(command, v)
		case Dependency:
			deps = append(deps, v)
		default:
			return "", fmt.Errorf("exec arguments must be strings or dep values, got %T", arg)
		}
	}
	if len(command) == 0 {
		return "", fmt.Errorf("exec needs a command")
	}

	ts, err := s.templateSet()
	if err != nil {
		return "", err
	}

	h := sha256.New()
	fmt.Fprintf(h, "exec-v1\x00%s\x00", strings.Join(command, "\x00"))
	fmt.Fprintf(h, "templates:\x00%s\x00", ts.currentClosureHash())
	for _, dep := range deps {
		fmt.Fprintf(h, "dep:%s:%s\x00", dep.Spec, s.hashDependency(dep))
	}
	fmt.Fprintf(h, "root:%s", s.Config.RootDir)
	key := hex.EncodeToString(h.Sum(nil))

	if out, ok := s.execCache.Get(key); ok {
		return out, nil
	}

	timeout := time.Duration(s.Config.CommandTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = s.Config.RootDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("exec %s: timed out after %s", strings.Join(command, " "), timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("exec %s: %s", strings.Join(command, " "), msg)
	}

	out := stdout.String()
	if err := s.execCache.Put(key, out); err != nil {
		return "", err
	}
	return out, nil
}

func (s *Site) hashDependency(dep Dependency) string {
	s.depMu.Lock()
	if h, ok := s.depHashes[dep.Spec]; ok {
		s.depMu.Unlock()
		return h
	}
	s.depMu.Unlock()

	h, _ := s.computeDependencyHash(dep.Spec)

	s.depMu.Lock()
	s.depHashes[dep.Spec] = h
	s.depMu.Unlock()
	return h
}

func (s *Site) resetDependencyHashes() {
	s.depMu.Lock()
	s.depHashes = make(map[string]string)
	s.depMu.Unlock()
}

func (s *Site) computeDependencyHash(spec string) (string, error) {
	kind, value, _ := strings.Cut(spec, ":")
	switch kind {
	case "file":
		data, err := os.ReadFile(s.resolveDepPath(value))
		if err != nil {
			return hashBytes([]byte("error:" + err.Error())), nil
		}
		return hashBytes(data), nil
	case "dir":
		return hashDirectory(s.resolveDepPath(value))
	case "data":
		if value == "" {
			data, err := json.Marshal(s.Data)
			if err != nil {
				return hashBytes([]byte(err.Error())), nil
			}
			return hashBytes(data), nil
		}
		data, err := json.Marshal(s.Data[value])
		if err != nil {
			return hashBytes([]byte(err.Error())), nil
		}
		return hashBytes(data), nil
	case "git":
		if value == "" {
			value = "."
		}
		return hashGitRepository(s.resolveDepPath(value))
	default:
		return hashBytes([]byte("unknown:" + spec)), nil
	}
}

func (s *Site) resolveDepPath(path string) string {
	if path == "" || filepath.IsAbs(path) || isRemote(path) {
		return path
	}
	return filepath.Join(s.Config.RootDir, path)
}

func hashDirectory(dir string) (string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != dir && info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return hashBytes([]byte("error:" + err.Error())), nil
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, _ := filepath.Rel(dir, path)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s:%d:", filepath.ToSlash(rel), len(data))
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashGitRepository(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	run := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return "error:" + err.Error()
		}
		return out.String()
	}

	h := sha256.New()
	fmt.Fprintf(h, "head:%s\x00", run("rev-parse", "HEAD"))
	fmt.Fprintf(h, "status:%s", run("status", "--porcelain=v1"))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// commandCache stores command output keyed by a dependency-sensitive hash.
type commandCache struct {
	dir  string
	mu   sync.Mutex
	used map[string]bool
}

func newCommandCache(dir string) *commandCache {
	return &commandCache{dir: dir, used: make(map[string]bool)}
}

func (c *commandCache) path(key string) string {
	return filepath.Join(c.dir, key+".out")
}

func (c *commandCache) Get(key string) (string, bool) {
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return "", false
	}
	c.mu.Lock()
	c.used[key] = true
	c.mu.Unlock()
	return string(data), true
}

func (c *commandCache) Put(key, output string) error {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return err
	}
	path := c.path(key)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(output), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	c.mu.Lock()
	c.used[key] = true
	c.mu.Unlock()
	return nil
}

func (c *commandCache) GC() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	c.mu.Lock()
	used := make(map[string]bool, len(c.used))
	for key := range c.used {
		used[key+".out"] = true
	}
	c.mu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tmp") {
			_ = os.Remove(filepath.Join(c.dir, name))
			continue
		}
		if strings.HasSuffix(name, ".out") && !used[name] {
			_ = os.Remove(filepath.Join(c.dir, name))
		}
	}
	return nil
}

func (s *Site) templateCommandDeps() []string {
	seen := make(map[string]bool)
	var specs []string
	dirs := []string{s.Config.LayoutsDir}
	for _, theme := range s.Config.ThemeChain() {
		dirs = append(dirs, filepath.Join(theme, "layouts"))
	}
	for _, dir := range dirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, m := range depPattern.FindAllSubmatch(data, -1) {
				spec := string(m[1])
				if !seen[spec] {
					seen[spec] = true
					specs = append(specs, spec)
				}
			}
			return nil
		})
	}
	sort.Strings(specs)
	return specs
}
