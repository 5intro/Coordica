package nodeservice

import (
	"context"
	"coordica/internal/localfile"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

func windowsXMLText(raw []byte) string {
	wide := make([]uint16, (len(raw)-2)/2)
	for i := range wide {
		wide[i] = binary.LittleEndian.Uint16(raw[2+i*2:])
	}
	return strings.Replace(string(utf16.Decode(wide)), `encoding="UTF-16"`, `encoding="UTF-8"`, 1)
}
func TestPlansUseFixedArgvAndEscapeSpecialPaths(t *testing.T) {
	spec := Spec{Executable: `/应用 & "test" $cash%token/bin/coordica`, DataDir: `/用户/数据 & "q" $cash%token`, RuntimePath: `/node 中文:/bin`}
	r := runtimeInfo{Home: t.TempDir(), UID: "501", SID: "S-1-5-21-1234", Path: spec.RuntimePath, Manager: "fixed-manager"}
	r.Platform = "darwin"
	p, err := buildPlan(spec, r)
	if err != nil {
		t.Fatal(err)
	}
	var plist struct {
		Args []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal(p.Content, &plist); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plist.Args, []string{spec.Executable, "runner", "--data", spec.DataDir}) {
		t.Fatalf("launchd altered argv: %#v", plist.Args)
	}
	if !strings.Contains(string(p.Content), xmlString(spec.RuntimePath)) {
		t.Fatal("launchd PATH missing")
	}
	r.Platform = "linux"
	p, err = buildPlan(spec, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p.Content), `ExecStart="/应用 & \"test\" $$cash%%token/bin/coordica" runner --data "/用户/数据 & \"q\" $$cash%%token"`) {
		t.Fatalf("systemd expansion not escaped: %s", p.Content)
	}
	if !strings.Contains(string(p.Content), `Environment="PATH=/node 中文:/bin"`) {
		t.Fatal("systemd PATH missing")
	}
	r.Platform = "windows"
	spec.Executable = `C:\Users\中文\Coordica & $money\coordica.exe`
	spec.DataDir = `C:\Users\中文\data space & ^$\`
	p, err = buildPlan(spec, r)
	if err != nil {
		t.Fatal(err)
	}
	var task struct {
		Command   string `xml:"Actions>Exec>Command"`
		Arguments string `xml:"Actions>Exec>Arguments"`
		User      string `xml:"Principals>Principal>UserId"`
		Level     string `xml:"Principals>Principal>RunLevel"`
		Logon     string `xml:"Principals>Principal>LogonType"`
		Policy    string `xml:"Settings>MultipleInstancesPolicy"`
	}
	if err := xml.Unmarshal([]byte(windowsXMLText(p.Content)), &task); err != nil {
		t.Fatal(err)
	}
	if task.Command != spec.Executable || task.Arguments != `runner --data "C:\Users\中文\data space & ^$\\"` {
		t.Fatalf("Windows argv altered: %+v", task)
	}
	if task.User != r.SID || task.Level != "LeastPrivilege" || task.Logon != "InteractiveToken" || task.Policy != "IgnoreNew" {
		t.Fatalf("wrong Windows privilege/identity: %+v", task)
	}
	if !reflect.DeepEqual(p.Preflight.Args, []string{"/?"}) {
		t.Fatal("Windows preflight enumerates unrelated tasks")
	}
	if windowsQuote(`a\"b\`) != `"a\\\"b\\"` {
		t.Fatal("Windows quote/backslash convention failed")
	}
}

func TestWindowsRejectsTaskSchedulerEnvironmentExpansionInPaths(t *testing.T) {
	r := runtimeInfo{Platform: "windows", Home: t.TempDir(), SID: "S-1-5-21-1234", Manager: "fixture"}
	for _, spec := range []Spec{{Executable: `C:\%TEMP%\coordica.exe`, DataDir: `C:\data`}, {Executable: `C:\bin\coordica.exe`, DataDir: `C:\data%literal`}} {
		if _, err := buildPlan(spec, r); err == nil {
			t.Fatal("Task Scheduler expansion path accepted")
		}
	}
}

func TestLinuxHonorsUserConfigDirectory(t *testing.T) {
	configHome := t.TempDir()
	p, err := buildPlan(Spec{Executable: "/bin/coordica", DataDir: "/data"}, runtimeInfo{Platform: "linux", Home: "/other-home", ConfigHome: configHome, Manager: "fixture"})
	if err != nil || !strings.HasPrefix(p.ConfigPath, configHome+string(os.PathSeparator)) {
		t.Fatalf("XDG_CONFIG_HOME ignored: %s %v", p.ConfigPath, err)
	}
}

func TestServiceConfigurationRejectsControlCharacters(t *testing.T) {
	_, err := buildPlan(Spec{Executable: "/bin/coordica", DataDir: "/data\nExecStart=evil"}, runtimeInfo{Platform: "linux", Home: t.TempDir(), Manager: "fixed"})
	if err == nil {
		t.Fatal("unit injection accepted")
	}
}

func TestInstallationFixturesNeverExecuteRealManagers(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			data := t.TempDir()
			spec := Spec{Executable: "/absolute/coordica", DataDir: data}
			r := runtimeInfo{Platform: platform, Home: home, UID: "501", SID: "S-1-5-21-1234", Path: "/node path:/bin", Manager: "fixture-manager"}
			var calls []command
			targetQueries := 0
			run := func(ctx context.Context, c command) (string, error) {
				calls = append(calls, c)
				if platform == "darwin" && len(c.Args) == 2 && c.Args[0] == "print" && strings.Contains(c.Args[1], "/local.") {
					targetQueries++
					if targetQueries == 1 {
						return "", errNotRegistered
					}
				}
				if reflect.DeepEqual(c.Args, mustPlan(t, spec, r).Probe.Args) {
					p := mustPlan(t, spec, r)
					if _, err := os.Stat(p.ConfigPath); os.IsNotExist(err) {
						return "", errNotRegistered
					}
					return fixtureRegistration(spec, r, p), nil
				}
				return "", nil
			}
			status, err := install(context.Background(), spec, r, run)
			if err != nil || status.Name == "" || (status.State != "started" && status.State != "activation_requested") {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			p, err := buildPlan(spec, r)
			if err != nil {
				t.Fatal(err)
			}
			if content, err := os.ReadFile(p.ConfigPath); err != nil || string(content) != string(p.Content) {
				t.Fatal("service definition missing", err)
			}
			for _, c := range calls {
				if c.Program != "fixture-manager" && !(platform == "windows" && c.Program == p.Probe.Program && reflect.DeepEqual(c.Args, p.Probe.Args)) {
					t.Fatal("unexpected executable boundary", c)
				}
			}
			if _, err := install(context.Background(), spec, r, run); err != nil {
				t.Fatal("service retry failed", err)
			}
			if platform == "darwin" {
				bootstraps := 0
				for _, c := range calls {
					if c.Args[0] == "bootstrap" {
						bootstraps++
					}
				}
				if bootstraps != 1 {
					t.Fatal("reused launchd service reinstalled")
				}
			}
		})
	}
}

func TestMissingManagerAndActivationFailureAreExplicit(t *testing.T) {
	r := runtimeInfo{Platform: "linux", Home: t.TempDir(), Manager: "fixture", Path: "/bin"}
	spec := Spec{Executable: "/absolute/coordica", DataDir: t.TempDir()}
	status, err := install(context.Background(), spec, r, func(context.Context, command) (string, error) { return "", errors.New("no user bus") })
	if err == nil || status.State != "failed" {
		t.Fatal("missing manager reported success")
	}
	p, _ := buildPlan(spec, r)
	if _, err := os.Stat(p.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unavailable manager still installed service")
	}
	status, err = install(context.Background(), spec, r, func(ctx context.Context, c command) (string, error) {
		if reflect.DeepEqual(c.Args, p.Probe.Args) {
			return "", errNotRegistered
		}
		if c.Args[1] == "start" {
			return "", errors.New("activation rejected")
		}
		return "", nil
	})
	if err == nil || status.State != "failed" {
		t.Fatal("failed start reported active")
	}
	if _, err := os.Stat(p.ConfigPath); err != nil {
		t.Fatal("retry definition missing")
	}
}

func mustPlan(t *testing.T, spec Spec, r runtimeInfo) plan {
	t.Helper()
	p, e := buildPlan(spec, r)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func fixtureRegistration(spec Spec, r runtimeInfo, p plan) string {
	switch r.Platform {
	case "darwin":
		return "path = " + p.ConfigPath + "\nprogram = " + spec.Executable + "\narguments = {\n" + strings.Join(fixedArgs(spec), "\n") + "\n}\n"
	case "linux":
		return "LoadState=loaded\nFragmentPath=" + p.ConfigPath + "\nDropInPaths=\nExecStart={ path=" + spec.Executable + " ; argv[]=" + strings.Join(fixedArgs(spec), " ") + " ; ignore_errors=no ; }\n"
	case "windows":
		return windowsXMLText(p.Content)
	}
	return ""
}

func TestRegisteredUnknownServicesNeverWriteReplaceOrActivate(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, saved := range []bool{false, true} {
			t.Run(platform+strconv.FormatBool(saved), func(t *testing.T) {
				spec := Spec{Executable: "/fixed/coordica", DataDir: t.TempDir(), CenterConfig: "/fixed/host.json"}
				r := runtimeInfo{Platform: platform, Home: t.TempDir(), UID: "501", SID: "S-1-5-21-123", Manager: "fixture", Path: "/bin"}
				p := mustPlan(t, spec, r)
				if saved {
					if e := localfile.WritePrivate(p.ConfigPath, p.Content); e != nil {
						t.Fatal(e)
					}
				}
				calls := 0
				status, e := install(context.Background(), spec, r, func(_ context.Context, c command) (string, error) {
					calls++
					if reflect.DeepEqual(c.Args, p.Preflight.Args) {
						return "", nil
					}
					if reflect.DeepEqual(c.Args, p.Probe.Args) {
						return strings.ReplaceAll(fixtureRegistration(spec, r, p), spec.Executable, "/unrelated/executable"), nil
					}
					t.Fatal("unknown service mutated or activated", c)
					return "", nil
				})
				if e == nil || status.State != "failed" || calls != 2 {
					t.Fatal("ownership not enforced", status, e, calls)
				}
				after, err := os.ReadFile(p.ConfigPath)
				if saved && (err != nil || string(after) != string(p.Content)) || !saved && !os.IsNotExist(err) {
					t.Fatal("conflicting registration changed disk")
				}
			})
		}
	}
}
func TestServiceQueryFailureIsNotAbsenceAndWindowsNeverForceReplaces(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		spec := Spec{Executable: "/fixed/coordica", DataDir: t.TempDir()}
		r := runtimeInfo{Platform: platform, Home: t.TempDir(), UID: "501", SID: "S-1-5-21-123", Manager: "fixture"}
		p := mustPlan(t, spec, r)
		if platform == "windows" && strings.Contains(strings.Join(p.Enable.Args, " "), "/F") {
			t.Fatal("force replacement remains")
		}
		_, e := install(context.Background(), spec, r, func(_ context.Context, c command) (string, error) {
			if reflect.DeepEqual(c.Args, p.Preflight.Args) {
				return "", nil
			}
			if reflect.DeepEqual(c.Args, p.Probe.Args) {
				return "", errors.New("permission denied")
			}
			t.Fatal("unverified service activated", c)
			return "", nil
		})
		if e == nil {
			t.Fatal("query failure treated as absent")
		}
	}
}
func TestRegisteredServiceActionsAndPrivilegeMustMatch(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		spec := Spec{Executable: "/fixed/coordica", DataDir: t.TempDir(), CenterConfig: "/fixed/host.json"}
		r := runtimeInfo{Platform: platform, Home: t.TempDir(), UID: "501", SID: "S-1-5-21-123", Manager: "fixture"}
		p := mustPlan(t, spec, r)
		valid := fixtureRegistration(spec, r, p)
		if !registeredMatches(spec, r, p, valid) {
			t.Fatal("owned registration refused", platform)
		}
		for _, bad := range []string{strings.ReplaceAll(valid, spec.CenterConfig, "/other/host.json"), strings.ReplaceAll(valid, "--host-config", "--run"), ""} {
			if registeredMatches(spec, r, p, bad) {
				t.Fatal("different fixed action admitted", platform)
			}
		}
		if platform == "windows" && registeredMatches(spec, r, p, strings.Replace(valid, "</Actions>", "<SendEmail><Server>unrelated</Server></SendEmail></Actions>", 1)) {
			t.Fatal("extra unknown task action admitted")
		}
		if platform == "windows" && registeredMatches(spec, r, p, strings.ReplaceAll(valid, "LeastPrivilege", "HighestAvailable")) {
			t.Fatal("different privilege admitted")
		}
		if platform == "linux" && registeredMatches(spec, r, p, strings.ReplaceAll(valid, "DropInPaths=", "DropInPaths=/other/override.conf")) {
			t.Fatal("unowned override admitted")
		}
	}
}

func TestRegistrationAbsenceRequiresExplicitManagerEvidence(t *testing.T) {
	for _, tc := range []struct {
		OS     string
		Exit   int
		Output string
		Absent bool
	}{
		{"darwin", 113, "Could not find service local.coordica", true}, {"darwin", 113, "Could not find domain gui/501", false}, {"darwin", 1, "Could not find service", false},
		{"windows", 1, "ERROR: The system cannot find the file specified.", false}, {"windows", 1, "Access denied", false}, {"windows", 1, "0x80070002", false},
		{"linux", 4, "LoadState=not-found\nFragmentPath=\nExecStart=\nDropInPaths=", true}, {"linux", 4, "Access denied", false},
	} {
		if registrationAbsent(tc.OS, &managerError{ExitCode: tc.Exit, Output: tc.Output}) != tc.Absent {
			t.Fatal(tc)
		}
	}
}
