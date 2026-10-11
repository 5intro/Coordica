package nodeservice

import (
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"os"
	"reflect"
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
			run := func(ctx context.Context, c command) error {
				calls = append(calls, c)
				if platform == "darwin" && len(c.Args) == 2 && c.Args[0] == "print" && strings.Contains(c.Args[1], "/local.") {
					targetQueries++
					if targetQueries == 1 {
						return errors.New("not loaded")
					}
				}
				return nil
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
				if c.Program != "fixture-manager" {
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
	status, err := install(context.Background(), spec, r, func(context.Context, command) error { return errors.New("no user bus") })
	if err == nil || status.State != "failed" {
		t.Fatal("missing manager reported success")
	}
	p, _ := buildPlan(spec, r)
	if _, err := os.Stat(p.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unavailable manager still installed service")
	}
	status, err = install(context.Background(), spec, r, func(ctx context.Context, c command) error {
		if c.Args[1] == "start" {
			return errors.New("activation rejected")
		}
		return nil
	})
	if err == nil || status.State != "failed" {
		t.Fatal("failed start reported active")
	}
	if _, err := os.Stat(p.ConfigPath); err != nil {
		t.Fatal("retry definition missing")
	}
}
