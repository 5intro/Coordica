package domain

import (
	"strings"
	"testing"
)

func TestWorkNotesCannotBeCommandsOrUnpinnedReferences(t *testing.T) {
	valid := WorkNoteInput{Anchor: WorkAnchor{Kind: "task", ID: "task_one"}, ExpectedStamp: strings.Repeat("a", 64),
		Kind: "note", OriginalText: "Explicitly saved discussion", References: []WorkReference{}}
	if err := ValidateWorkNote(valid); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"command", "url", "missing stamp", "unbounded", "composite kind", "duplicate", "reference text"} {
		t.Run(mode, func(t *testing.T) {
			in := valid
			switch mode {
			case "command":
				in.Kind = "approve"
			case "url":
				in.Anchor.ID = "https://example.invalid/task"
			case "missing stamp":
				in.ExpectedStamp = ""
			case "unbounded":
				in.OriginalText = strings.Repeat("x", (16<<10)+1)
			case "composite kind":
				in.References = []WorkReference{{Kind: "project|requirement", ID: "req_one", Stamp: strings.Repeat("a", 64)}}
			case "duplicate":
				ref := WorkReference{Kind: "task", ID: "task_one", Stamp: strings.Repeat("a", 64)}
				in.References = []WorkReference{ref, ref}
			case "reference text":
				in.Kind = "reference"
				in.References = []WorkReference{{Kind: "task", ID: "task_one", Stamp: strings.Repeat("a", 64)}}
			}
			if err := ValidateWorkNote(in); err == nil {
				t.Fatal("invalid coordination input accepted", mode)
			}
		})
	}
}
