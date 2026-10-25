package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Characterization: the same bytes replay by digest while the lease is valid.
// This is not HTTP Idempotency-Key support, nor permission after lease expiry.
func TestArtifactSameBytesReplayWithinLease(t *testing.T) {
	f := newServiceFixture(t)
	a := f.claim(t, "artifact-replay")
	ctx := context.Background()
	first, err := f.s.UploadArtifact(ctx, f.node, a.Run.ID, strings.NewReader("fixed bytes"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	second, err := f.s.UploadArtifact(ctx, f.node, a.Run.ID, strings.NewReader("fixed bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("receipt changed: %+v != %+v", first, second)
	}
	f.now = a.Run.LeaseUntil
	if _, err = f.s.UploadArtifact(ctx, f.node, a.Run.ID, strings.NewReader("fixed bytes")); err == nil {
		t.Fatal("expired lease accepted replay")
	}
}
