package domain

import (
	"path"
	"strings"
)

// Check paths are Owner policy, not a task's write permission. Inputs are
// literal repository paths; outputs permit only disposable untracked files.
func ValidateCheckPaths(check Check) error {
	for _, list := range []struct {
		paths   []string
		literal bool
	}{{check.RequiredPaths, true}, {check.OutputPaths, false}} {
		if len(list.paths) > 128 {
			return Fail("invalid_input", "check inputs and outputs are limited to 128 paths each")
		}
		seen := map[string]bool{}
		for _, name := range list.paths {
			if !ChatText(name, 4096, true) || strings.ContainsAny(name, "\\\x00\r\n:") ||
				strings.HasPrefix(name, "/") || seen[name] {
				return Fail("invalid_input", "check paths must be unique repository-relative paths")
			}
			seen[name] = true
			for _, component := range strings.Split(name, "/") {
				if component == "" || component == "." || component == ".." || strings.EqualFold(component, ".git") {
					return Fail("invalid_input", "check paths cannot include dot, parent or Git metadata components")
				}
				if _, err := path.Match(component, ""); err != nil || list.literal && strings.ContainsAny(component, "*?[") {
					return Fail("invalid_input", "required check inputs must be literal paths; outputs may use valid globs")
				}
			}
		}
	}
	return nil
}

func CheckOutputs(checks []Check) []string {
	var outputs []string
	seen := map[string]bool{}
	for _, check := range checks {
		for _, output := range check.OutputPaths {
			if !seen[output] {
				outputs = append(outputs, output)
				seen[output] = true
			}
		}
	}
	return outputs
}
