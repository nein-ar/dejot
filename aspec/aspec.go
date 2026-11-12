package aspec

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var includePattern = regexp.MustCompile(`^\s*%-\s*include\s+(.+?)\s*-%\s*$`)

var varDefPattern = regexp.MustCompile(`^\s*%-\s*([a-zA-Z_]\w*)\s*=\s*"((?:\\.|[^"\\])*)"\s*-%\s*$`)

var varRefPattern = regexp.MustCompile(`%-([a-zA-Z_]\w*)-%`)

func Expand(srcPath string) ([]byte, map[string]string, error) {
	source, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, nil, err
	}

	resolvedPath, err := filepath.Abs(srcPath)
	if err != nil {
		return nil, nil, err
	}
	if evalPath, evalErr := filepath.EvalSymlinks(resolvedPath); evalErr == nil {
		resolvedPath = evalPath
	} else if !os.IsNotExist(evalErr) {
		return nil, nil, evalErr
	}

	dir := filepath.Dir(srcPath)
	seen := make(map[string]bool)
	seen[resolvedPath] = true
	vars := make(map[string]string)
	params := make(map[string]string)

	expanded, collectedVars, collectedParams, err := expandSourceWithVars(source, dir, seen, vars, params)
	if err != nil {
		return nil, nil, err
	}

	result, err := substituteVariables(expanded, collectedVars)
	if err != nil {
		return nil, nil, err
	}

	return result, collectedParams, nil
}

func ExpandSource(source []byte, basePath string) ([]byte, map[string]string, error) {
	seen := make(map[string]bool)
	vars := make(map[string]string)
	params := make(map[string]string)
	expanded, collectedVars, collectedParams, err := expandSourceWithVars(source, basePath, seen, vars, params)
	if err != nil {
		return nil, nil, err
	}
	result, err := substituteVariables(expanded, collectedVars)
	return result, collectedParams, err
}

func expandSourceWithVars(source []byte, basePath string, seen map[string]bool, vars map[string]string, params map[string]string) ([]byte, map[string]string, map[string]string, error) {
	lines := bytes.Split(source, []byte("\n"))
	var result bytes.Buffer

	for i, line := range lines {
		lineStr := string(line)
		trimmedStr := strings.TrimSpace(lineStr)

		includeMatches := includePattern.FindStringSubmatch(trimmedStr)
		varMatches := varDefPattern.FindStringSubmatch(trimmedStr)

		if includeMatches != nil && len(includeMatches) > 1 {
			includePath := strings.TrimSpace(includeMatches[1])

			resolvedPath, err := filepath.Abs(filepath.Join(basePath, includePath))
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid include path %q: %w", includePath, err)
			}

			evalSymPath, evalSymErr := filepath.EvalSymlinks(resolvedPath)
			if evalSymErr == nil {
				resolvedPath = evalSymPath
			} else if !os.IsNotExist(evalSymErr) {
				return nil, nil, nil, fmt.Errorf("cannot resolve include path %q: %w", includePath, evalSymErr)
			}

			if seen[resolvedPath] {
				return nil, nil, nil, fmt.Errorf("circular include detected: %s", resolvedPath)
			}

			if _, err := os.Stat(resolvedPath); err != nil {
				return nil, nil, nil, fmt.Errorf("include file not found: %q (resolved to %q): %w", includePath, resolvedPath, err)
			}

			seen[resolvedPath] = true

			includedSource, err := os.ReadFile(resolvedPath)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("cannot read include file %q: %w", resolvedPath, err)
			}

			expandedContent, collectedVars, collectedParams, err := expandSourceWithVars(includedSource, filepath.Dir(resolvedPath), seen, vars, params)
			if err != nil {
				return nil, nil, nil, err
			}

			for k, v := range collectedVars {
				vars[k] = v
			}
			for k, v := range collectedParams {
				params[k] = v
			}

			trimmed := bytes.TrimLeft(expandedContent, " \t\r\n")
			result.Write(trimmed)

			if i < len(lines)-1 {
				if !bytes.HasSuffix(trimmed, []byte("\n\n")) {
					if !bytes.HasSuffix(trimmed, []byte("\n")) {
						result.WriteString("\n\n")
					} else {
						result.WriteByte('\n')
					}
				}
			}

		} else if varMatches != nil && len(varMatches) == 3 {
			keyName := varMatches[1]
			keyValue := varMatches[2]
			vars[keyName] = keyValue

		} else {
			result.Write(line)
			if i < len(lines)-1 {
				result.WriteByte('\n')
			}
		}
	}

	resBytes := bytes.TrimRight(result.Bytes(), "\n")
	resBytes = append(resBytes, '\n')
	return resBytes, vars, params, nil
}

func substituteVariables(source []byte, vars map[string]string) ([]byte, error) {
	sourceStr := string(source)

	result := varRefPattern.ReplaceAllStringFunc(sourceStr, func(match string) string {
		varName := match[2 : len(match)-2]

		if value, ok := vars[varName]; ok {
			return value
		}

		return match
	})

	matches := varRefPattern.FindAllStringSubmatch(result, -1)
	var undefinedVars []string
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) == 2 {
			varName := match[1]
			if _, ok := vars[varName]; !ok && !seen[varName] {
				undefinedVars = append(undefinedVars, varName)
				seen[varName] = true
			}
		}
	}

	if len(undefinedVars) > 0 {
		return nil, fmt.Errorf("undefined variables: %s", strings.Join(undefinedVars, ", "))
	}

	return []byte(result), nil
}
