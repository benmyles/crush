package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// DefaultContextPaths returns the built-in instruction/context path
// candidates in priority order.
func DefaultContextPaths() []string {
	return slices.Clone(defaultContextPaths)
}

// IsDefaultContextPath reports whether path is one of Crush's built-in
// instruction/context path candidates.
func IsDefaultContextPath(path string) bool {
	normalized := normalizeContextPath(path)
	for _, candidate := range defaultContextPaths {
		if normalizeContextPath(candidate) == normalized {
			return true
		}
	}
	return false
}

// SelectedAgentInstructionsPath returns the first existing built-in
// instructions file in priority order.
func SelectedAgentInstructionsPath(workingDir string, contextPaths []string) (string, bool) {
	enabled := make(map[string]bool, len(contextPaths))
	for _, path := range contextPaths {
		enabled[normalizeContextPath(path)] = true
	}

	for _, candidate := range defaultContextPaths {
		if !enabled[normalizeContextPath(candidate)] {
			continue
		}
		fullPath := candidate
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(workingDir, fullPath)
		}
		info, err := statExactPath(fullPath)
		if err != nil || info.IsDir() {
			continue
		}
		return fullPath, true
	}
	return "", false
}

func statExactPath(path string) (os.FileInfo, error) {
	cleaned := filepath.Clean(path)
	dir := filepath.Dir(cleaned)
	base := filepath.Base(cleaned)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name() != base {
			continue
		}
		return entry.Info()
	}
	return nil, os.ErrNotExist
}

func normalizeContextPath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	return strings.TrimSuffix(strings.ToLower(path), "/")
}

func appendContextPaths(base, extra []string) []string {
	paths := make([]string, 0, len(base)+len(extra))
	for _, path := range append(slices.Clone(base), extra...) {
		if slices.Contains(paths, path) {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}
