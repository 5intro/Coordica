package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ValidateProfilePaths keeps local discovery output separate from credentials.
// Resolve existing parents too, so a not-yet-created file under a directory
// alias cannot overwrite a role asset later.
func ValidateProfilePaths(discovery, data, chat string) error {
	paths := []string{discovery, chat}
	if data != "" {
		paths = append(paths, filepath.Join(data, "runner.json"))
	}
	for i, path := range paths {
		if path == "" {
			continue
		}
		if strings.ContainsAny(path, "\x00\r\n") {
			return fmt.Errorf("invalid role asset path")
		}
		if i == 0 && strings.EqualFold(filepath.Base(path), "runner.json") {
			return fmt.Errorf("a discovery profile cannot replace paired runner.json identity")
		}
		a, err := resolvedProfilePath(path)
		if err != nil {
			return err
		}
		for _, other := range paths[i+1:] {
			if other == "" {
				continue
			}
			b, err := resolvedProfilePath(other)
			if err != nil {
				return err
			}
			x, ex := os.Stat(path)
			y, ey := os.Stat(other)
			if a == b || (runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "ios") && strings.EqualFold(a, b) || ex == nil && ey == nil && os.SameFile(x, y) {
				return fmt.Errorf("discovery, interaction and paired Runner role assets must use distinct paths")
			}
		}
	}
	return nil
}

func resolvedProfilePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		base, err := resolvedProfilePath(parent)
		return filepath.Join(base, filepath.Base(path)), err
	}
	return resolved, err
}
