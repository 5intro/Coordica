package testreport

import (
	"strings"
	"testing"
)

func TestGoCaseRequiresCompleteActualResults(t *testing.T) {
	run := `{"Action":"run","Package":"p","Test":"TestA"}` + "\n"
	pass := `{"Action":"pass","Package":"p","Test":"TestA"}` + "\n"
	end := `{"Action":"pass","Package":"p"}` + "\n"
	for _, tc := range []struct{ name, output, want string }{
		{"complete", run + pass + end, "passed"},
		{"repeat", run + pass + run + pass + end, "passed"},
		{"interrupted repeat", run + pass + run + end, "unreported"},
		{"clipped", run + pass, "unreported"},
		{"command only", "PASS\n", "unreported"},
		{"not run", pass + end, "unreported"},
		{"missing", end, "unrun"},
		{"failed package", run + pass + `{"Action":"fail","Package":"p"}`, "incomplete_suite"},
		{"repeat fails", run + pass + run + `{"Action":"fail","Package":"p","Test":"TestA"}` + "\n" + end, "failed"},
		{"skip", run + `{"Action":"skip","Package":"p","Test":"TestA"}` + "\n" + end, "skipped"},
		{"malformed", run + "{\n" + pass + end, "unreported"},
		{"truncated bound", strings.Repeat(" ", 256<<10) + run + pass + end, "unreported"},
		{"other package", strings.ReplaceAll(run+pass+end, `"p"`, `"other"`), "unreported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoCase(tc.output, "p#TestA"); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGoCaseRequiresTheLastStreamToFinish(t *testing.T) {
	start := `{"Action":"start","Package":"p"}` + "\n"
	run := `{"Action":"run","Package":"p","Test":"TestA"}` + "\n"
	pass := `{"Action":"pass","Package":"p","Test":"TestA"}` + "\n"
	end := `{"Action":"pass","Package":"p"}` + "\n"
	first := start + run + pass + end
	for _, tc := range []struct{ name, second, want string }{
		{"clipped next stream", start + run + pass, "unreported"},
		{"next test without start", run + pass, "unreported"},
		{"next unrelated test", start + `{"Action":"run","Package":"p","Test":"TestB"}`, "unreported"},
		{"complete second", start + run + pass + end, "passed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoCase(first+tc.second, "p#TestA"); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
