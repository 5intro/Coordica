package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPhysicalSizeSignalsDoNotTriggerAutomaticRepair(t *testing.T) {
	var dense strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&dense, "class C%d { void work() {} } ", i)
	}
	for _, fixture := range []struct{ name, path, source string }{
		{"dense ownership", "src/Dense.java", dense.String()},
		{"PowerShell module", "src/Module.psm1", strings.Repeat("\n", 501)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			f := newITFixture(t)
			qualityCandidateSource(t, f, fixture.path, []byte(fixture.source))
			before := f.snapshot(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := f.w.Tick(ctx); err != nil {
				t.Fatal("deterministic regression became an operational retry", err)
			}
			after := f.snapshot(t)
			if f.candidate(t).State != "prechecking" || after.Tasks[0].Repairs != before.Tasks[0].Repairs || f.candidate(t).Maintainability == "" {
				t.Fatal("advisory lost or incorrectly triggered repair", after.Tasks, after.Candidates)
			}
			if after.Project.UsedSeconds != before.Project.UsedSeconds || after.Project.ReservedSeconds != before.Project.ReservedSeconds {
				t.Fatal("advisory changed cumulative spending")
			}
			if err := f.w.Tick(ctx); err != nil || f.snapshot(t).Tasks[0].Repairs != after.Tasks[0].Repairs {
				t.Fatal("advisory consumed a repair", err)
			}
			main, err := f.g.Query(ctx, f.remote, "refs/heads/main")
			if err != nil || main != f.b {
				t.Fatal("unreviewed source moved main", main, err)
			}
		})
	}
}
