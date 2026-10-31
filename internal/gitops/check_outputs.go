package gitops

import (
	"fmt"
	"strings"
)

// UndeclaredCheckOutputs distinguishes disposable-output policy from a changed
// fixed source tree. Ignored files still need explicit trusted declarations.
type UndeclaredCheckOutputs struct {
	Paths []string
	Count int
}

func (e *UndeclaredCheckOutputs) Error() string {
	return fmt.Sprintf("%v: %d undeclared check outputs: %s", ErrDirty, e.Count, strings.Join(e.Paths, ", "))
}
func (e *UndeclaredCheckOutputs) Unwrap() error { return ErrDirty }
