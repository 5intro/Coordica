//go:build !windows

package process

import (
	"errors"
	"syscall"
	"testing"
)

func TestExitSignalRaceRequiresPositiveQuiescenceEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stopped bool
		signals []error
		want    error
	}{
		{"exiting group now empty", true, []error{syscall.EPERM}, nil},
		{"group still alive or unknown", false, []error{syscall.EPERM}, syscall.EPERM},
		{"other failure retained", true, []error{syscall.EPERM, syscall.EIO}, syscall.EIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stopped, err := groupStopResult(tc.stopped, tc.signals...)
			if stopped != tc.stopped || !errors.Is(err, tc.want) {
				t.Fatal("signal error changed stop evidence", stopped, err)
			}
		})
	}
}
