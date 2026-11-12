package ssg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

const manifestVersion = "ssg-1"

type manifestEntry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type manifest struct {
	Version     string                   `json:"version"`
	Fingerprint string                   `json:"fingerprint"`
	Entries     map[string]manifestEntry `json:"entries"`
}

func newManifest() *manifest {
	return &manifest{Version: manifestVersion, Entries: make(map[string]manifestEntry)}
}

func loadManifest(path string) *manifest {
	m := newManifest()
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	var loaded manifest
	if err := json.Unmarshal(data, &loaded); err != nil || loaded.Version != manifestVersion {
		return m
	}
	if loaded.Entries == nil {
		loaded.Entries = make(map[string]manifestEntry)
	}
	return &loaded
}

func saveManifest(path string, m *manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func manifestPath(cfg *Config) string {
	return filepath.Join(cfg.CacheDir, "ssg-manifest.json")
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *manifest) complete(outputDir string) bool {
	if m.Fingerprint == "" {
		return false
	}
	for rel := range m.Entries {
		if _, err := os.Stat(filepath.Join(outputDir, filepath.FromSlash(rel))); err != nil {
			return false
		}
	}
	return true
}
