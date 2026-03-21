package dejot

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nein-ar/dejot/params"
	"github.com/nein-ar/dejot/parser"
	"github.com/nein-ar/dejot/render/html"
)

func TestReferenceSuite(t *testing.T) {
	testDir := "tests"
	files, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("failed to read test directory: %v", err)
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".test") {
			continue
		}

		t.Run(file.Name(), func(t *testing.T) {
			path := filepath.Join(testDir, file.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read test file %s: %v", path, err)
			}

			runTestFile(t, string(content))
		})
	}
}

func runTestFile(t *testing.T, content string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			ticks := ""
			for j := 0; j < len(line) && line[j] == '`'; j++ {
				ticks += "`"
			}
			options := strings.TrimSpace(line[len(ticks):])
			i++

			var inputBuilder strings.Builder
			hasLuaFilter := false
			for i < len(lines) && lines[i] != "." && lines[i] != "!" {
				inputBuilder.WriteString(lines[i])
				inputBuilder.WriteString("\n")
				i++
			}

			if i < len(lines) && strings.TrimSpace(lines[i]) == "!" {
				hasLuaFilter = true
				i++
				for i < len(lines) && lines[i] != "." {
					i++
				}
			}

			if i < len(lines) && lines[i] == "." {
				i++
			}

			var outputBuilder strings.Builder
			for i < len(lines) && !strings.HasPrefix(lines[i], ticks) {
				outputBuilder.WriteString(lines[i])
				outputBuilder.WriteString("\n")
				i++
			}
			i++

			input := inputBuilder.String()
			expected := outputBuilder.String()

			t.Run(fmt.Sprintf("line_%d", i), func(t *testing.T) {
				if strings.Contains(options, "a") {
					t.Skip("AST expectations not implemented")
					return
				}
				if strings.Contains(options, "!") || hasLuaFilter {
					t.Skip("Lua filters not implemented")
					return
				}

				doc := parser.Parse([]byte(input))
				var buf bytes.Buffer
				renderer := html.NewHTMLRenderer(doc, params.Params{})
				renderer.Render(&buf)

				actual := buf.String()

				actual = strings.TrimSpace(actual)
				expected = strings.TrimSpace(expected)

				if actual != expected {
					t.Errorf("\nInput:\n%s\nExpected:\n%s\nActual:\n%s", input, expected, actual)
				}
			})
		} else {
			i++
		}
	}
}
