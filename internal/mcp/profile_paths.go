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

// ValidateCenterPaths separates co-located service identities and their fixed
// discovery output, including aliases through existing parent directories.
func ValidateCenterPaths(discovery, runner, chat, center, config string, fixedAssets ...string) error {
	if err := ValidateProfilePaths(discovery, runner, chat); err != nil {
		return err
	}
	paths := []string{runner, discovery, chat, config, center, filepath.Join(runner, "runner.json")}
	for _, asset := range fixedAssets {
		if asset != "" {
			paths = append(paths, asset)
		}
	}
	resolved := make([]string, len(paths))
	for i, p := range paths {
		if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n") {
			return fmt.Errorf("absolute fixed role paths required")
		}
		r, err := resolvedProfilePath(p)
		if err != nil {
			return err
		}
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			r = strings.ToLower(r)
		}
		resolved[i] = r
	}
	within := func(a, b string) bool {
		rel, err := filepath.Rel(b, a)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if within(resolved[0], resolved[4]) || within(resolved[4], resolved[0]) {
		return fmt.Errorf("center and Runner data directories must be separate")
	}
	for _, i := range []int{1, 2, 3} {
		if within(resolved[i], resolved[4]) || i != 1 && within(resolved[i], resolved[0]) {
			return fmt.Errorf("fixed role asset conflicts with service data")
		}
	}
	for i := 1; i < len(paths); i++ {
		if i == 4 {
			continue
		}
		for j := i + 1; j < len(paths); j++ {
			if j == 4 {
				continue
			}
			a, ea := os.Stat(paths[i])
			b, eb := os.Stat(paths[j])
			if resolved[i] == resolved[j] || ea == nil && eb == nil && os.SameFile(a, b) {
				return fmt.Errorf("fixed role assets must be distinct")
			}
		}
	}
	return nil
}
