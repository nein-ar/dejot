package ssg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	htmltmpl "html/template"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RenderContext is the value handed to every template.
type RenderContext struct {
	Site    *Site
	Page    *Page
	Pages   []*Page
	Data    map[string]any
	Params  map[string]any
	Menus   map[string]any
	Content htmltmpl.HTML
	Body    htmltmpl.HTML

	Title       string
	Description string
}

func (s *Site) context(p *Page) *RenderContext {
	ctx := &RenderContext{
		Site:    s,
		Page:    p,
		Data:    s.Data,
		Menus:   s.Menus,
		Content: htmltmpl.HTML(p.Content),
	}
	if p != nil {
		ctx.Pages = p.Pages
		ctx.Params = p.Params
		ctx.Title = p.Title
		ctx.Description = stringValue(p.Params["description"])
	}
	if p != nil && p.isSection() {
		ctx.Pages = p.Pages
	}
	return ctx
}

type templateSet struct {
	site  *Site
	tmpl  *htmltmpl.Template
	names map[string]bool
	dirs  []string

	hashes  map[string]string
	refs    map[string][]string
	stack   []string
	stackMu sync.Mutex
}

var templateRefPattern = regexp.MustCompile(`{{-?\s*(?:template|partial)\s+"([^"]+)"`)

func (s *Site) loadTemplates() (*templateSet, error) {
	ts := &templateSet{
		site:   s,
		names:  make(map[string]bool),
		hashes: make(map[string]string),
		refs:   make(map[string][]string),
	}
	ts.tmpl = htmltmpl.New("__root__").Funcs(s.funcMap())
	ts.tmpl = ts.tmpl.Option("missingkey=zero")

	for _, theme := range s.Config.ThemeChain() {
		ts.dirs = append(ts.dirs, filepath.Join(theme, "layouts"))
	}
	if s.Config.LayoutsDir != "" {
		ts.dirs = append(ts.dirs, s.Config.LayoutsDir)
	}

	for _, dir := range ts.dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if p != dir && strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !isTemplateFile(info.Name()) {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if err := ts.parse(rel, data); err != nil {
				return fmt.Errorf("template %s: %w", p, err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return ts, nil
}

func isTemplateFile(name string) bool {
	return strings.HasSuffix(name, ".html") ||
		strings.HasSuffix(name, ".htm") ||
		strings.HasSuffix(name, ".tmpl") ||
		strings.HasSuffix(name, ".gotmpl")
}

func (ts *templateSet) parse(rel string, data []byte) error {
	names := []string{rel}
	if ext := path.Ext(rel); ext != "" {
		names = append(names, strings.TrimSuffix(rel, ext))
	}
	base := path.Base(rel)
	if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if !containsString(names, base) {
		names = append(names, base)
	}

	sum := hashBytes(data)
	var refs []string
	for _, m := range templateRefPattern.FindAllSubmatch(data, -1) {
		refs = append(refs, string(m[1]))
	}
	for _, name := range names {
		if _, err := ts.tmpl.New(name).Parse(string(data)); err != nil {
			return err
		}
		ts.names[name] = true
		ts.hashes[name] = sum
		ts.refs[name] = refs
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// lookup finds a template by its common names. Explicit path wins over a bare
// file name so themes can keep same-named partials in different folders.
func (ts *templateSet) lookup(name string) string {
	if name == "" {
		return ""
	}
	candidates := []string{name}
	if path.Ext(name) == "" {
		candidates = append(candidates, name+".html")
	}
	if !strings.HasPrefix(name, "partials/") {
		candidates = append(candidates, "partials/"+name, "partials/"+name+".html")
	}
	if !strings.HasPrefix(name, "_partials/") {
		candidates = append(candidates, "_partials/"+name, "_partials/"+name+".html")
	}
	for _, c := range candidates {
		if ts.names[c] {
			return c
		}
	}
	return ""
}

func (ts *templateSet) execute(name string, ctx any) (templateOutput, error) {
	var buf bytes.Buffer
	if name == "" {
		return templateOutput{}, fmt.Errorf("template not found")
	}
	ts.push(name)
	defer ts.pop()
	if err := ts.tmpl.ExecuteTemplate(&buf, name, ctx); err != nil {
		return templateOutput{}, err
	}
	return templateOutput(buf.Bytes()), nil
}

func (ts *templateSet) push(name string) {
	ts.stackMu.Lock()
	ts.stack = append(ts.stack, name)
	ts.stackMu.Unlock()
}

func (ts *templateSet) pop() {
	ts.stackMu.Lock()
	if len(ts.stack) > 0 {
		ts.stack = ts.stack[:len(ts.stack)-1]
	}
	ts.stackMu.Unlock()
}

func (ts *templateSet) currentClosureHash() string {
	ts.stackMu.Lock()
	name := ""
	if len(ts.stack) > 0 {
		name = ts.stack[len(ts.stack)-1]
	}
	ts.stackMu.Unlock()

	visited := make(map[string]bool)
	var walk func(string)
	walk = func(n string) {
		if n == "" || visited[n] {
			return
		}
		visited[n] = true
		for _, ref := range ts.refs[n] {
			resolved := ts.lookup(ref)
			if resolved == "" {
				resolved = ref
			}
			walk(resolved)
		}
	}
	if name != "" {
		walk(name)
	} else {
		for n := range ts.hashes {
			walk(n)
		}
	}

	names := make([]string, 0, len(visited))
	for n := range visited {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s:%s\x00", n, ts.hashes[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}

type templateOutput []byte

func (s *Site) layoutFor(p *Page, ts *templateSet) (layout, base string) {
	var candidates []string
	if p.Layout != "" {
		candidates = append(candidates, p.Layout, "_default/"+p.Layout)
	}
	section := ""
	if p.Section != nil && p.Section.RelPath != "" {
		section = strings.TrimPrefix(p.Section.RelPath, "/")
	}
	if section != "" {
		candidates = append(candidates, section+"/"+p.Kind, section+"/single", section+"/list")
	}
	if t := stringValue(p.Params["type"]); t != "" {
		candidates = append(candidates, t, "_default/"+t)
	}
	switch p.Kind {
	case "home":
		candidates = append(candidates, "home", "index", "_default/home", "_default/index", "_default/list")
	case "section":
		candidates = append(candidates, "list", "section", "_default/list", "_default/section")
	default:
		candidates = append(candidates, "single", "page", "_default/single", "_default/page")
	}
	if layout == "" {
		for _, c := range candidates {
			if ts.names[c] {
				layout = c
				break
			}
		}
	}

	for _, b := range []string{"_default/base", "base", "_base"} {
		if ts.names[b] {
			base = b
			break
		}
	}
	return layout, base
}

func (s *Site) renderPage(p *Page, ts *templateSet) ([]byte, error) {
	layout, base := s.layoutFor(p, ts)
	if layout == "" {
		return nil, fmt.Errorf("no template for %s (kind %s, layout %q)", p.Path, p.Kind, p.Layout)
	}
	ctx := s.context(p)
	out, err := ts.execute(layout, ctx)
	if err != nil {
		return nil, err
	}
	if base == "" || base == layout || boolValue(p.Params["no_base"]) {
		return []byte(out), nil
	}
	ctx.Body = htmltmpl.HTML(out)
	wrapped, err := ts.execute(base, ctx)
	if err != nil {
		return nil, err
	}
	return []byte(wrapped), nil
}

func (s *Site) funcMap() htmltmpl.FuncMap {
	return htmltmpl.FuncMap{
		"dict":           dict,
		"list":           func(v ...any) []any { return v },
		"default":        defaultValue,
		"upper":          strings.ToUpper,
		"lower":          strings.ToLower,
		"title":          strings.Title,
		"trim":           strings.TrimSpace,
		"trimSpace":      strings.TrimSpace,
		"trimPrefix":     strings.TrimPrefix,
		"trimSuffix":     strings.TrimSuffix,
		"hasPrefix":      strings.HasPrefix,
		"hasSuffix":      strings.HasSuffix,
		"contains":       strings.Contains,
		"replace":        strings.ReplaceAll,
		"split":          strings.Split,
		"join":           func(sep string, v any) string { return joinValue(sep, v) },
		"json":           toJSON,
		"toJSON":         toJSON,
		"jsonify":        toJSON,
		"safeHTML":       func(s string) htmltmpl.HTML { return htmltmpl.HTML(s) },
		"safeURL":        func(s string) htmltmpl.URL { return htmltmpl.URL(s) },
		"safeCSS":        func(s string) htmltmpl.CSS { return htmltmpl.CSS(s) },
		"safeJS":         func(s string) htmltmpl.JS { return htmltmpl.JS(s) },
		"absURL":         s.absURL,
		"relURL":         s.relURL,
		"asset":          s.assetURL,
		"href":           href,
		"add":            add,
		"sub":            sub,
		"mul":            mul,
		"div":            div,
		"mod":            mod,
		"min":            minInt,
		"percent":        percent,
		"urlquery":       url.QueryEscape,
		"max":            maxInt,
		"now":            time.Now,
		"date":           dateFormat,
		"formatTime":     formatTime,
		"timeAgo":        timeAgo,
		"within":         within,
		"formatDuration": formatDuration,
		"formatBytes":    formatBytes,
		"plural":         plural,
		"first":          first,
		"last":           last,
		"reverse":        reverse,
		"sortBy":         sortBy,
		"where":          where,
		"uniq":           uniq,
		"partial":        s.partial,
		"exec":           s.execCommand,
		"dep":            s.dep,
		"djot": func(src string) (htmltmpl.HTML, error) {
			out, err := renderDjot([]byte(src), s.Config.RootDir)
			return htmltmpl.HTML(out), err
		},
		"fileExt": func(s string) string { return strings.TrimPrefix(strings.ToLower(path.Ext(s)), ".") },
	}
}

func dict(values ...any) map[string]any {
	m := make(map[string]any, len(values)/2)
	for i := 0; i+1 < len(values); i += 2 {
		m[stringValue(values[i])] = values[i+1]
	}
	return m
}

func defaultValue(def, value any) any {
	if isEmpty(value) {
		return def
	}
	return value
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

func joinValue(sep string, v any) string {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return ""
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return stringValue(v)
	}
	parts := make([]string, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		parts[i] = stringValue(rv.Index(i).Interface())
	}
	return strings.Join(parts, sep)
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *Site) absURL(u string) string {
	if u == "" {
		return s.Config.BaseURL
	}
	if isRemote(u) || strings.HasPrefix(u, "//") {
		return u
	}
	return strings.TrimSuffix(s.Config.BaseURL, "/") + "/" + strings.TrimPrefix(u, "/")
}

func (s *Site) assetURL(name string) string {
	if name == "" {
		return ""
	}
	if isRemote(name) || strings.HasPrefix(name, "/") {
		return name
	}
	if assets, ok := s.Config.Params["assets_url"].(string); ok && assets != "" {
		return strings.TrimSuffix(assets, "/") + "/" + strings.TrimPrefix(name, "/")
	}
	return s.absURL(name)
}

func href(currentPath, target string) string {
	if target == "" {
		return ""
	}
	if isRemote(target) || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "tel:") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "?") {
		return target
	}
	if !strings.HasPrefix(target, "/") {
		return target
	}

	suffix := ""
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		suffix = target[i:]
		target = target[:i]
	}
	if target == "/" {
		target = "/index.html"
	}
	if strings.HasSuffix(target, "/") {
		target += "index.html"
	}

	baseDir := path.Dir(strings.TrimPrefix(currentPath, "/"))
	if baseDir == "." {
		baseDir = ""
	}
	rel, err := filepath.Rel(baseDir, strings.TrimPrefix(target, "/"))
	if err != nil {
		return "/" + strings.TrimPrefix(target, "/") + suffix
	}
	rel = filepath.ToSlash(rel)
	if rel == "" {
		rel = "."
	}
	return rel + suffix
}

func add(a, b any) int64 { return toInt(a) + toInt(b) }
func sub(a, b any) int64 { return toInt(a) - toInt(b) }
func mul(a, b any) int64 { return toInt(a) * toInt(b) }
func div(a, b any) int64 {
	d := toInt(b)
	if d == 0 {
		return 0
	}
	return toInt(a) / d
}
func mod(a, b any) int64 {
	d := toInt(b)
	if d == 0 {
		return 0
	}
	return toInt(a) % d
}
func minInt(a, b any) int64 {
	x, y := toInt(a), toInt(b)
	if x < y {
		return x
	}
	return y
}
func maxInt(a, b any) int64 {
	x, y := toInt(a), toInt(b)
	if x > y {
		return x
	}
	return y
}

func percent(part, total any) float64 {
	t := toFloatSafe(total)
	if t == 0 {
		return 0
	}
	return toFloatSafe(part) / t * 100
}

func toFloatSafe(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	case float32:
		return float64(x)
	case uint64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func toInt(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case uint:
		return int64(x)
	case uint64:
		return int64(x)
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	case string:
		i, _ := strconv.ParseInt(x, 10, 64)
		return i
	}
	return 0
}

func dateFormat(format string, v any) string {
	t := coerceTime(v)
	if t.IsZero() {
		return ""
	}
	return t.Format(format)
}

func formatTime(v any) string {
	t := coerceTime(v)
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func timeAgo(v any) string {
	t := coerceTime(v)
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < 0 {
		return t.Format("2006-01-02")
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	days := int(d.Hours()) / 24
	if days < 30 {
		return fmt.Sprintf("%dd ago", days)
	}
	return t.Format("2006-01-02")
}

func within(v any, window string) bool {
	d, err := time.ParseDuration(window)
	if err != nil {
		return false
	}
	t := coerceTime(v)
	if t.IsZero() {
		return false
	}
	delta := time.Since(t)
	return delta >= 0 && delta < d
}

func coerceTime(v any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case *time.Time:
		if x != nil {
			return *x
		}
	case int64:
		if x > 0 {
			return time.Unix(x, 0)
		}
	case int:
		if x > 0 {
			return time.Unix(int64(x), 0)
		}
	case float64:
		if x > 0 {
			return time.Unix(int64(x), 0)
		}
	case string:
		return timeValue(x)
	}
	return time.Time{}
}

func formatDuration(v any) string {
	sec := toInt(v)
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%dm %ds", sec/60, sec%60)
	}
	return fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60)
}

func formatBytes(v any) string {
	b := toInt(v)
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func plural(v any, unit string) string {
	n := toInt(v)
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func first(n int, v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return v
	}
	if n > rv.Len() {
		n = rv.Len()
	}
	return rv.Slice(0, n).Interface()
}

func last(n int, v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return v
	}
	if n > rv.Len() {
		n = rv.Len()
	}
	return rv.Slice(rv.Len()-n, rv.Len()).Interface()
}

func reverse(v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) || rv.Len() < 2 {
		return v
	}
	out := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), rv.Len(), rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out.Index(i).Set(rv.Index(rv.Len() - 1 - i))
	}
	return out.Interface()
}

func sortBy(key string, v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return v
	}
	out := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), rv.Len(), rv.Len())
	reflect.Copy(out, rv)
	sort.SliceStable(out.Interface(), func(i, j int) bool {
		a := lookupField(out.Index(i).Interface(), key)
		b := lookupField(out.Index(j).Interface(), key)
		return compareValues(a, b) < 0
	})
	return out.Interface()
}

func where(key string, value any, v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return v
	}
	out := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		if reflect.DeepEqual(lookupField(item, key), value) {
			out = reflect.Append(out, rv.Index(i))
		}
	}
	return out.Interface()
}

func uniq(v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return v
	}
	out := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), 0, rv.Len())
	seen := make(map[string]bool)
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		key := fmt.Sprint(item)
		if !seen[key] {
			seen[key] = true
			out = reflect.Append(out, rv.Index(i))
		}
	}
	return out.Interface()
}

func lookupField(v any, key string) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		mv := rv.MapIndex(reflect.ValueOf(key))
		if mv.IsValid() {
			return mv.Interface()
		}
	case reflect.Struct:
		f := rv.FieldByName(key)
		if f.IsValid() && f.CanInterface() {
			return f.Interface()
		}
	}
	return nil
}

func compareValues(a, b any) int {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	as, bs := stringValue(a), stringValue(b)
	return strings.Compare(as, bs)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func (s *Site) partial(name string, ctx any) (htmltmpl.HTML, error) {
	ts, err := s.templateSet()
	if err != nil {
		return "", err
	}
	resolved := ts.lookup(name)
	if resolved == "" {
		return "", fmt.Errorf("partial %q not found", name)
	}
	out, err := ts.execute(resolved, ctx)
	if err != nil {
		return "", err
	}
	return htmltmpl.HTML(out), nil
}

// templateSet is created once per build. The tiny indirection keeps partial
// callable from inside templates, where there is no build object to close over.
func (s *Site) templateSet() (*templateSet, error) {
	if s.ts != nil {
		return s.ts, nil
	}
	ts, err := s.loadTemplates()
	if err != nil {
		return nil, err
	}
	s.ts = ts
	return ts, nil
}
