package params

import (
	"sync"
)

type Params map[string]string

func (p Params) Get(key, def string) string {
	if v, ok := p[key]; ok {
		return v
	}
	return def
}

var (
	registry = make(map[string][]string)
	mu       sync.RWMutex
)

func RegisterRenderer(name string, supportedParams []string) {
	mu.Lock()
	defer mu.Unlock()

	for _, param := range supportedParams {
		if _, ok := registry[param]; !ok {
			registry[param] = []string{}
		}
		registry[param] = append(registry[param], name)
	}
}

func KnownParam(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := registry[name]
	return ok
}

func SupportedBy(rendererName, paramName string) bool {
	mu.RLock()
	defer mu.RUnlock()

	renderers, ok := registry[paramName]
	if !ok {
		return false
	}

	for _, r := range renderers {
		if r == rendererName {
			return true
		}
	}
	return false
}
