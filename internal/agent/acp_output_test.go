package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestACPFinalOutputExcludesPreToolCommentary(t *testing.T) {
	for _, tool := range []string{"dsh", "cursor", "kimi"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			commentary := false
			result, err := Execute(ctx, fixtureRequest(t, tool, "commentary"), func(event Event) {
				commentary = commentary || strings.Contains(string(event.Data), "inspect the source")
			})
			if err != nil || result.OutputText != "done" || !result.TreeStopped || !commentary {
				t.Fatalf("terminal output and activity stream were conflated: result=%+v error=%v", result, err)
			}
		})
	}
}
