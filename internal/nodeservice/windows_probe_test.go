package nodeservice

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"coordica/internal/localfile"
)

func taskProbeReply(t *testing.T, name, stage string, code uint32, xml string) []byte {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"version": 1, "task_name": name, "stage": stage, "hresult": code, "xml": xml})
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestWindowsTaskProbeNumericErrorsIgnoreDiagnosticLanguage(t *testing.T) {
	name := "coordica-center-0123456789abcdef"
	for _, message := range []string{"错误: 系统找不到指定的文件。", "ERROR: The system cannot find the file specified.", "Erreur : accès refusé.", "0x80070002", "0x8004130f"} {
		if registrationAbsent("windows", &managerError{ExitCode: 1, Output: message}) {
			t.Fatal("unstructured text classified as native absence", message)
		}
		if _, e := decodeWindowsTaskProbe([]byte(message), name); e == nil || errors.Is(e, errNotRegistered) {
			t.Fatal("localized/unknown stdout admitted", message, e)
		}
	}
	for _, tc := range []struct {
		stage  string
		code   uint32
		absent bool
	}{
		{"get_task", 0x80070002, true}, {"get_task", 0x80070005, false}, {"get_task", 0x8004130f, false}, {"get_task", 0x80004005, false},
		{"connect", 0x80070002, false}, {"folder", 0x80070002, false}, {"xml", 0x80070002, false}, {"registered", 0x80070002, false}, {"unknown", 0x80070002, false},
	} {
		_, e := decodeWindowsTaskProbe(taskProbeReply(t, name, tc.stage, tc.code, ""), name)
		if e == nil || errors.Is(e, errNotRegistered) != tc.absent {
			t.Fatal(tc, e)
		}
	}
	for _, raw := range [][]byte{
		taskProbeReply(t, "coordica-runner-0123456789abcdef", "get_task", 0x80070002, ""),
		taskProbeReply(t, name, "get_task", 0x80070002, "<Task/>"),
		[]byte(`{"version":1,"task_name":"` + name + `","stage":"get_task","xml":""}`),
		[]byte(`{"version":1,"task_name":"` + name + `","stage":"get_task","hresult":2147942402,"xml":"","unknown":true}`),
		[]byte(`{"version":1,"task_name":"` + name + `","stage":"get_task","hresult":2147942402,"hresult":0,"xml":""}`),
	} {
		if _, e := decodeWindowsTaskProbe(raw, name); e == nil || errors.Is(e, errNotRegistered) {
			t.Fatal("invalid or different-task envelope classified absent", string(raw), e)
		}
	}
}

func TestWindowsNumericProbePreservesRegisteredXMLAndStrictActions(t *testing.T) {
	spec := Spec{Executable: `C:\工具\coordica.exe`, DataDir: `C:\数据\Runner`, CenterConfig: `C:\配置\center.json`}
	r := runtimeInfo{Platform: "windows", Home: t.TempDir(), SID: "S-1-5-21-123", Manager: "fixture-schtasks"}
	p := mustPlan(t, spec, r)
	xml := windowsXMLText(p.Content)
	parsed, e := decodeWindowsTaskProbe(taskProbeReply(t, p.Name, "registered", 0, xml), p.Name)
	if e != nil || parsed != xml || !registeredMatches(spec, r, p, parsed) {
		t.Fatal("registered Unicode action damaged", e)
	}
	for _, bad := range []string{strings.ReplaceAll(xml, spec.CenterConfig, `C:\其它\center.json`), strings.ReplaceAll(xml, "LeastPrivilege", "HighestAvailable"), strings.ReplaceAll(xml, r.SID, "S-1-5-18"), strings.Replace(xml, "</Actions>", "<SendEmail/></Actions>", 1)} {
		parsed, e = decodeWindowsTaskProbe(taskProbeReply(t, p.Name, "registered", 0, bad), p.Name)
		if e != nil || registeredMatches(spec, r, p, parsed) {
			t.Fatal("registered conflicting action or privilege admitted", e)
		}
	}
	// The existing UTF-16 registered-definition parser remains in force.
	if !registeredMatches(spec, r, p, string(p.Content)) {
		t.Fatal("UTF-16 registered task damaged", e)
	}
}

func TestWindowsTaskProbeIsStaticFixedLocalReadOnlyArgv(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "System32", "schtasks.exe")
	first := windowsTaskProbe(manager, "coordica-center-0123456789abcdef")
	second := windowsTaskProbe(manager, "coordica-runner-fedcba9876543210")
	if first.Program != filepath.Join(filepath.Dir(manager), "WindowsPowerShell", "v1.0", "powershell.exe") || !reflect.DeepEqual(first.Args, second.Args) || !reflect.DeepEqual(first.Args[:4], []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand"}) || first.WindowsTask == second.WindowsTask {
		t.Fatal("probe command controlled by task input", first, second)
	}
	raw, e := base64.StdEncoding.DecodeString(first.Args[4])
	if e != nil || len(raw)%2 != 0 {
		t.Fatal(e)
	}
	wide := make([]uint16, len(raw)/2)
	for i := range wide {
		wide[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	script := string(utf16.Decode(wide))
	if script != windowsTaskProbeScript || strings.Contains(script, first.WindowsTask) || !strings.Contains(script, "$service.Connect()") || !strings.Contains(script, "$folder.GetTask($name)") {
		t.Fatal("script interpolates input or changes local query")
	}
	for _, bad := range []string{"RegisterTask", "DeleteTask", "GetTasks(", ".Run(", "Invoke-Expression"} {
		if strings.Contains(script, bad) {
			t.Fatal("probe mutates/enumerates tasks or evaluates data", bad)
		}
	}
	if _, e = runCommand(context.Background(), command{Program: "must-not-execute", WindowsTask: "unrelated; injected"}); e == nil || !strings.Contains(e.Error(), "invalid fixed") {
		t.Fatal("invalid task input reached process boundary", e)
	}
}

func TestWindowsFreshNumericAbsenceInstallsButUnknownAndAccessErrorsDoNot(t *testing.T) {
	for _, code := range []uint32{0x80070002, 0x80070005, 0x80004005} {
		t.Run(fmt.Sprintf("%08x", code), func(t *testing.T) {
			spec := Spec{Executable: "/fixed/coordica", DataDir: t.TempDir()}
			r := runtimeInfo{Platform: "windows", Home: t.TempDir(), SID: "S-1-5-21-123", Manager: "fixture"}
			p := mustPlan(t, spec, r)
			activations := 0
			status, e := install(context.Background(), spec, r, func(_ context.Context, c command) (string, error) {
				if reflect.DeepEqual(c.Args, p.Preflight.Args) {
					return "", nil
				}
				if reflect.DeepEqual(c.Args, p.Probe.Args) {
					return decodeWindowsTaskProbe(taskProbeReply(t, p.Name, "get_task", code, ""), p.Name)
				}
				if code != 0x80070002 {
					t.Fatal("unknown/access-denied probe reached mutation", c)
				}
				activations++
				return "", nil
			})
			if code == 0x80070002 {
				if e != nil || status.State != "activation_requested" || activations != 2 {
					t.Fatal("fresh numeric absence failed", status, e, activations)
				}
			} else {
				if e == nil || activations != 0 {
					t.Fatal("unproved absence activated", e)
				}
				if _, e = os.Stat(p.ConfigPath); !os.IsNotExist(e) {
					t.Fatal("failed ownership query wrote config", e)
				}
			}
		})
	}
}

func TestDarwinPreflightDoesNotEnumerateGUIJobs(t *testing.T) {
	for _, center := range []bool{false, true} {
		spec := Spec{Executable: "/fixed/coordica", DataDir: t.TempDir()}
		if center {
			spec.CenterConfig = "/fixed/center.json"
		}
		r := runtimeInfo{Platform: "darwin", Home: t.TempDir(), UID: "501", Manager: "/bin/launchctl"}
		p := mustPlan(t, spec, r)
		if p.Preflight.Program != "/bin/launchctl" || !reflect.DeepEqual(p.Preflight.Args, []string{"print-disabled", "gui/501"}) || !reflect.DeepEqual(p.Probe.Args, []string{"print", "gui/501/" + p.Name}) {
			t.Fatal("preflight collects GUI inventory or loses exact service probe", p.Preflight, p.Probe)
		}
		if e := localfile.WritePrivate(p.ConfigPath, p.Content); e != nil {
			t.Fatal(e)
		}
		_, e := install(context.Background(), spec, r, func(_ context.Context, c command) (string, error) {
			if reflect.DeepEqual(c.Args, []string{"print", "gui/501"}) {
				t.Fatal("full GUI inventory queried")
			}
			if reflect.DeepEqual(c.Args, p.Preflight.Args) {
				return "disabled services = {}", nil
			}
			if reflect.DeepEqual(c.Args, p.Probe.Args) {
				return fixtureRegistration(spec, r, p), nil
			}
			return "", nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
}
