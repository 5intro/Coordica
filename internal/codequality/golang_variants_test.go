package codequality

import (
	"fmt"
	"testing"
)

func receiverSource(prefix, name string, count int) string {
	source := prefix + "package sample\n"
	for i := 0; i < count; i++ {
		source += fmt.Sprintf("func (w *Worker) %s%d() {}\n", name, i)
	}
	return source
}

func TestGoReceiverMethodsRespectCompatibleBuildVariants(t *testing.T) {
	for _, fixture := range []struct {
		name, first, second, left, right string
		fail                             bool
	}{
		{"OS suffix", "worker_linux.go", "worker_windows.go", "", "", false},
		{"arch suffix", "worker_linux_amd64.go", "worker_linux_arm64.go", "", "", false},
		{"custom tag", "left.go", "right.go", "//go:build feature\n\n", "//go:build !feature\n\n", false},
		{"boolean tags", "left.go", "right.go", "//go:build a && b\n\n", "//go:build a && !b\n\n", false},
		{"compatible tags", "left.go", "right.go", "//go:build a\n\n", "//go:build b\n\n", true},
		{"mixed assignment", "left.go", "right.go", "//go:build a\n\n", "//go:build !b\n\n", true},
		{"legacy tags", "left.go", "right.go", "// +build feature\n\n", "// +build !feature\n\n", false},
		{"BOM", "left.go", "right.go", "\ufeff//go:build feature\n\n", "//go:build !feature\n\n", false},
		{"block comment is not a directive", "left.go", "right.go", "/*\n//go:build feature\n*/\n", "//go:build !feature\n\n", true},
		{"case-sensitive filename", "worker_LINUX.go", "worker_windows.go", "", "", true},
		{"filename before first dot", "worker_linux.legacy.go", "worker_windows.go", "", "", false},
		{"unix and windows", "left.go", "right.go", "//go:build unix\n\n", "//go:build windows\n\n", false},
		{"release thresholds", "left.go", "right.go", "//go:build go1.25 && !go1.26\n\n", "//go:build go1.26\n\n", false},
		{"compiler", "left.go", "right.go", "//go:build gc\n\n", "//go:build gccgo\n\n", false},
		{"Android matches Linux suffix", "worker_linux.go", "worker_android.go", "", "", true},
		{"unix is not filename OS", "worker_unix.go", "worker_windows.go", "", "", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			files := []File{{"worker.go", "package sample\ntype Worker struct{}\n"},
				{fixture.first, receiverSource(fixture.left, "Left", 11)}, {fixture.second, receiverSource(fixture.right, "Right", 11)}}
			findings, err := Measure(files)
			if err != nil || hasRule(findings, "receiver_methods") != fixture.fail {
				t.Fatal("incompatible variants combined or real growth hidden", findings, err)
			}
		})
	}
}

func TestGoReceiverDebtCannotMoveBetweenBuildVariants(t *testing.T) {
	base := []File{{"worker.go", "package sample\ntype Worker struct{}\n"},
		{"worker_linux.go", receiverSource("", "Linux", 25)}, {"worker_windows.go", receiverSource("", "Windows", 21)}}
	next := append([]File(nil), base...)
	next[2].Text = receiverSource("", "Windows", 24)
	findings, err := Regressions(base, next)
	if err != nil || !hasRule(findings, "receiver_methods") {
		t.Fatal("larger Linux debt masked Windows growth", findings, err)
	}
	base = []File{{"worker.go", "package sample\ntype Worker struct{}\n" + receiverSource("", "Work", 25)[len("package sample\n"):]}}
	next = []File{{"worker.go", "//go:build feature\n\npackage sample\ntype Worker struct{}\n" +
		receiverSource("", "Work", 24)[len("package sample\n"):]}}
	findings, err = Regressions(base, next)
	if err != nil || len(findings) != 0 {
		t.Fatal("conditional cleanup borrowed no new allowance but was rejected", findings, err)
	}
}
