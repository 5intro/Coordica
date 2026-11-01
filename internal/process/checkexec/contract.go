package checkexec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coordica/internal/domain"
)

// Failure describes the failed operation, not whether product code is faulty.
type Failure struct {
	Kind  string
	Check string
	Cause error
}

func (f *Failure) Error() string { return fmt.Sprintf("check %s (%s): %v", f.Check, f.Kind, f.Cause) }
func (f *Failure) Unwrap() error { return f.Cause }

func requiredInputs(dir string, check domain.Check) error {
	if err := domain.ValidateCheckPaths(check); err != nil {
		return &Failure{Kind: "check_configuration", Check: check.Name, Cause: err}
	}
	for _, input := range check.RequiredPaths {
		current := dir
		for _, part := range strings.Split(input, "/") {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
				return &Failure{Kind: "check_inputs", Check: check.Name,
					Cause: fmt.Errorf("required input %q is missing or is not a regular file/directory", input)}
			}
		}
	}
	return nil
}
