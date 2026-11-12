package ssg

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func (s *Site) loadData() error {
	dir := s.Config.DataDir
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}

	return filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if p != dir && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".json" && ext != ".toml" && ext != ".yaml" && ext != ".yml" {
			return nil
		}

		value, err := readDataFile(p, ext)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = strings.TrimSuffix(filepath.ToSlash(rel), ext)
		parts := strings.Split(rel, "/")
		mergeData(s.Data, parts, value)
		return nil
	})
}

func readDataFile(path, ext string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("data %s: %w", path, err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("data %s: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("data %s: %w", path, err)
		}
	}
	return normalizeNumbers(value), nil
}

func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case float64:
		if math.Trunc(x) == x && x >= math.MinInt64 && x <= math.MaxInt64 {
			return int(x)
		}
		return x
	case float32:
		return normalizeNumbers(float64(x))
	case map[string]any:
		for k, item := range x {
			x[k] = normalizeNumbers(item)
		}
		return x
	case []any:
		for i, item := range x {
			x[i] = normalizeNumbers(item)
		}
		return x
	}
	return v
}

func mergeData(root map[string]any, parts []string, value any) {
	if len(parts) == 0 {
		return
	}
	key := parts[0]
	if len(parts) == 1 {
		root[key] = mergeValue(root[key], value)
		return
	}
	child, ok := root[key].(map[string]any)
	if !ok {
		child = make(map[string]any)
		root[key] = child
	}
	mergeData(child, parts[1:], value)
}

func mergeValue(old, new any) any {
	oldMap, okOld := old.(map[string]any)
	newMap, okNew := new.(map[string]any)
	if okOld && okNew {
		for k, v := range newMap {
			oldMap[k] = mergeValue(oldMap[k], v)
		}
		return oldMap
	}
	return new
}
