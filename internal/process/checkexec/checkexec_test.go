package checkexec

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"coordica/internal/domain"
)

func TestCommandSuiteFailureCancellationAndOutputBounds(t *testing.T) {
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		mode   string
		cancel bool
		passed bool
	}{
		{"pass", false, true}, {"fail", false, false}, {"output", false, true}, {"wait", true, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			check := domain.Check{Name: "helper", Command: binary, Args: []string{"-test.run=^TestCheckSubprocess$", "--", tc.mode}, TimeoutSeconds: 10}
			results, quiet, e := Run(ctx, t.TempDir(), []domain.Check{check}, os.Environ())
			if len(results) != 1 || results[0].Passed != tc.passed || !quiet || (e == nil) != tc.passed || len(results[0].Output) > 256<<10 {
				t.Fatal(results, quiet, e)
			}
		})
	}
}

func TestCheckSubprocess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			switch os.Args[i+1] {
			case "pass":
				fmt.Println("ok")
				os.Exit(0)
			case "fail":
				fmt.Println("failed")
				os.Exit(3)
			case "output":
				for n := 0; n < 40000; n++ {
					fmt.Println("test-output")
				}
				os.Exit(0)
			case "wait":
				time.Sleep(time.Minute)
				os.Exit(0)
			}
		}
	}
}
