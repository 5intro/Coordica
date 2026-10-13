package nodeservice

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestCenterPlansFixedArgumentsOnEveryOS(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			spec := Spec{Executable: `/fixed path/coordica`, DataDir: `/data & quotes`, CenterConfig: `/config & "quoted"/center.json`, HTTPSProxy: "http://127.0.0.1:7890"}
			r := runtimeInfo{Platform: platform, Home: t.TempDir(), ConfigHome: t.TempDir(), UID: "501", SID: "S-1-5-21-123", Manager: "fixed-manager", Path: "/bin:/native"}
			if platform == "windows" {
				spec.CenterConfig = `C:\中文\config path\center.json`
				spec.DataDir = `C:\data path`
				spec.Executable = `C:\installed\coordica.exe`
			}
			p, e := buildPlan(spec, r)
			if e != nil {
				t.Fatal(e)
			}
			content := string(p.Content)
			if platform == "windows" {
				content = windowsXMLText(p.Content)
			}
			if !strings.Contains(p.Name, "center") || strings.Contains(p.Name, "runner") {
				t.Fatal("center service ownership missing")
			}
			if strings.Contains(content, "<string>runner</string>") || strings.Contains(content, " runner --data ") {
				t.Fatal("center launches Runner")
			}
			if platform == "linux" {
				if !strings.Contains(content, "center-serve --host-config") || !strings.Contains(content, "HTTPS_PROXY=http://127.0.0.1:7890") {
					t.Fatal("fixed center args/proxy absent", content)
				}
			} else {
				var doc any
				_ = doc
				decoder := xml.NewDecoder(strings.NewReader(content))
				for {
					if _, e := decoder.Token(); e != nil {
						if e.Error() != "EOF" {
							t.Fatal(e)
						}
						break
					}
				}
				if !strings.Contains(content, "center-serve") || !strings.Contains(content, "--host-config") {
					t.Fatal("fixed center argument absent")
				}
			}
		})
	}
}
