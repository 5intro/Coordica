package process

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

func duplexInputHelper() int {
	// Fill the output pipe before reading stdin. A sequential writer/reader
	// deadlocks here once both directions exceed pipe capacity.
	if _, err := os.Stdout.Write(bytes.Repeat([]byte{'x'}, 1<<20)); err != nil {
		return 8
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 8
	}
	fmt.Fprintf(os.Stderr, "%x", sha256.Sum256(input))
	return 0
}

func TestCollectRawInputDrainsBothDirectionsAndDeliversEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := bytes.Repeat([]byte("source\x00input\n"), 1<<17)
	out, stderr, result, err := CollectRawInput(ctx, helperSpec(t, "duplex"), input, 1<<20, 256)
	if err != nil || !result.TreeStopped || result.ExitCode != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(out, bytes.Repeat([]byte{'x'}, 1<<20)) || string(stderr) != fmt.Sprintf("%x", sha256.Sum256(input)) {
		t.Fatal("duplex collection changed bytes or failed to deliver EOF", len(out), string(stderr))
	}
}

func TestCollectRawInputUnblocksWriterAfterOverflowOrCancellation(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprint(overflow), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			limit := 1 << 20
			if overflow {
				limit = 1024
			}
			spec := rawHelperSpec(t, "stdout-limit") // This process never reads stdin.
			out, _, result, err := CollectRawInput(ctx, spec, bytes.Repeat([]byte{'i'}, 1<<20), limit, 256)
			want := error(context.DeadlineExceeded)
			if overflow {
				want = ErrOutputLimit
			}
			if !errors.Is(err, want) || !result.Canceled || !result.TreeStopped || len(out) == 0 {
				t.Fatalf("writer did not join supervised termination: result=%+v err=%v", result, err)
			}
		})
	}
}
