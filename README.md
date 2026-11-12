# dejot

Djot parser, renderers and `ssg`, a static site generator.

`ssg` works as a library and as the `dejot-ssg` binary. It handles pages,
sections, data, templates, themes and outputs. It has no built in idea of a
blog, a port tree, a manpage or a package site.

## Library

```go
import "github.com/nein-ar/dejot/ssg"

cfg := ssg.NewConfig()
cfg.Title = "Example"
cfg.ContentDir = "content"
cfg.DataDir = "data"
cfg.ThemesDir = "themes"
cfg.Theme = "default"
cfg.OutputDir = "public"
cfg.CacheDir = ".cache"

site, err := ssg.Load(cfg)
if err != nil {
    return err
}
site.AddData("build", map[string]any{"commit": "abc123"})
site.AddOutput("index.json", []byte(`{"ok":true}`))

result, err := site.Build()
if result.Skipped {
    // no Djot was parsed and no template was executed
}
```

`ssg.Render` and `ssg.RenderFile` convert Djot to HTML without a site.

## Binary

```sh
go build ./cmd/dejot-ssg
./dejot-ssg build --config ssg.toml
./dejot-ssg serve --config ssg.toml --addr :1313
./dejot-ssg render page.djot
```

## Configuration

```toml
title = "Example"
base_url = "https://example.org/"
theme = "default"

content_dir = "content"
data_dir = "data"
themes_dir = "themes"
layouts_dir = "layouts"
static_dir = "static"
output_dir = "public"
cache_dir = ".cache"
ugly_urls = false
extensions = [".djot"]
index_file = "_index"

allow_commands = false
command_timeout_seconds = 60
command_deps = []

[params]
anything = "read from .Site.Params.anything"
```

Keys outside the core list are merged into `[params]`, so a site can keep its
own values at the top level.

## Content

A content file is optional front matter followed by Djot:

```djot
---
title = "Hello"
date = "2026-01-02"
layout = "single"
weight = 10
---

# Hello

This is *Djot*.
```

`---` front matter accepts TOML or YAML. `+++` is TOML. Templates also see front
matter keys as `.Page.Params.<key>`.

Paths map to pretty URLs:

| Source | Output |
| --- | --- |
| `content/_index.djot` | `index.html` |
| `content/about.djot` | `about/index.html` |
| `content/posts/_index.djot` | `posts/index.html` |
| `content/posts/hello.djot` | `posts/hello/index.html` |

Front matter can set `url = "/some/file.html"` for an exact output path, or
`slug` to change the last path part.

Djot includes and variables from `aspec` work in content:

```djot
%- include ../shared/header.djot -%
%-version = "1.2.3"-%
Released %-version-%.
```

## Data

Every `.json`, `.toml` or YAML file under `data_dir` becomes
`.Site.Data.<name>`. Nested directories become nested maps:

```text
data/team/people.json   -> .Site.Data.team.people
```

`site.AddData(name, value)` adds data in code and wins over a file with the same
name. JSON integers become `int`, so templates can compare them with integer
literals.

## Templates

Templates are Go `html/template` files under `layouts/` and
`themes/<theme>/layouts/`. The project `layouts/` directory wins when both have
a file with the same name. A page picks a layout from front matter `layout`,
then from its kind:

- `home`: `home`, `index`, `_default/home`, `_default/index`, `_default/list`
- `section`: `list`, `section`, `_default/list`
- `page`: `single`, `page`, `_default/single`

If `_default/base.html` exists, the selected layout renders first and its result
becomes `.Body` inside the base template. Set `no_base = true` in front matter
to skip that step.

The context contains:

| Field | Meaning |
| --- | --- |
| `.Site` | `Title`, `Description`, `BaseURL`, `Params`, `Data`, `Pages`, `Home`, `Menus` |
| `.Page` | `Title`, `Kind`, `Layout`, `Path`, `URL`, `RelURL`, `Params`, `Date`, `Content` |
| `.Content` | the page content as safe HTML |
| `.Body` | the rendered layout, used by the base template |
| `.Data` | `.Site.Data` |
| `.Params` | `.Page.Params` |
| `.Pages` | child pages of the current section |

Functions include `dict`, `list`, `default`, string helpers, `href`, `asset`,
`absURL`, `relURL`, `json`, `safeHTML`, `safeURL`, `date`, `formatTime`,
`timeAgo`, `within`, `formatBytes`, `formatDuration`, `plural`, `percent`, `min`,
`max`, `first`, `last`, `reverse`, `sortBy`, `where`, `uniq`, `partial`,
`urlquery`, `exec`, `dep` and `djot`.

`href CURRENT TARGET` makes a root relative link relative to the current output
path. `asset PATH` resolves a site-root path, or joins it to `params.assets_url`
when that is set. `djot SOURCE` renders a Djot string to safe HTML.

## Commands

Templates can run commands when `allow_commands = true`. The command runs in
the site root with no shell, so it receives arguments as written.

```gotemplate
{{ $table := exec "git" "-C" "ports" "log" "--format=%h %s" (dep "git:ports") }}
{{ djot $table }}
```

`exec` takes a command and arguments. A `dep` value is an input, not an
argument. The cache stores the result under a key built from:

- the command and its arguments
- the templates that call the command, including partials they name
- every declared dependency
- the site root

The cache means an unrelated CSS or content change does not run the command
again. A change to the command, its template closure or a declared dependency
does. Declare literal paths in `dep` so the build fingerprint sees them.
`command_deps` adds dependencies that only exist as runtime strings.

Dependency kinds:

- `file:PATH`: file content
- `dir:PATH`: file names and content below a directory, ignoring dot files and
  `.git`
- `data:NAME`: one data value, or all data when `NAME` is empty
- `git:PATH`: repository `HEAD` and working tree status

`command_timeout_seconds` limits a single command, with 60 seconds as the
default.

## Themes

```text
themes/default/
  theme.toml
  layouts/
  static/
```

A theme can extend another theme:

```toml
name = "xplshn"
extends = "base"
```

The child inherits every layout and static file it does not override. Static
files mirror the output root, so a theme that wants `/assets/x.css` puts the
file at `static/assets/x.css`. The generator assumes nothing about that layout.

## Caching

`Build` fingerprints the config, templates, content, data, static files,
command dependencies and queued outputs. When the fingerprint and the output
tree do not change, it returns `Skipped == true` before parsing Djot or running
a template.

When the fingerprint changes, the builder compares each output by content hash,
so it writes only files with different bytes. It deletes outputs that disappear,
along with empty directories. A theme switch leaves no stale files.

`Force` ignores the fingerprint. `DryRun` reports without writing.

## Outputs

`site.AddOutput(path, bytes)` and
`site.AddTemplateOutput(path, template, context)` add files that are not pages:
JSON indexes, feeds, generated scripts, sitemaps. They use the same cache and
cleanup as pages and static files.

## Conformance

The files under `tests/` are the upstream `djot.js` functional suite. `go test`
runs both HTML expectations and AST and source position expectations through
`render/asttext`. The test runner skips Lua filter tests because this module has
no Lua runtime.
