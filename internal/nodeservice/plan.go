package nodeservice

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

func xmlString(text string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(text))
	return b.String()
}

func buildPlan(spec Spec, r runtimeInfo) (plan, error) {
	for _, value := range []string{spec.Executable, spec.DataDir, r.Home, r.ConfigHome, r.Path, r.UID, r.SID, r.Manager} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return plan{}, errors.New("service configuration contains control characters")
		}
	}
	name := serviceName(spec.DataDir)
	cmd := func(args ...string) command { return command{Program: r.Manager, Args: args} }
	switch r.Platform {
	case "darwin":
		label := "local." + name
		domain := "gui/" + r.UID
		target := domain + "/" + label
		path := filepath.Join(r.Home, "Library", "LaunchAgents", label+".plist")
		log := filepath.Join(spec.DataDir, "runner-service.log")
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>runner</string><string>--data</string><string>%s</string></array>
<key>WorkingDirectory</key><string>%s</string>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>%s</string><key>HOME</key><string>%s</string></dict>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>5</integer>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, xmlString(label), xmlString(spec.Executable), xmlString(spec.DataDir), xmlString(spec.DataDir), xmlString(r.Path), xmlString(r.Home), xmlString(log), xmlString(log))
		return plan{Name: label, ConfigPath: path, LogPath: log, Content: []byte(content), Preflight: cmd("print", domain), Probe: cmd("print", target), Reload: cmd("bootout", target), Enable: cmd("bootstrap", domain, path), Start: cmd("kickstart", target), Verify: cmd("print", target)}, nil
	case "linux":
		unit := name + ".service"
		configHome := r.ConfigHome
		if configHome == "" {
			configHome = filepath.Join(r.Home, ".config")
		}
		path := filepath.Join(configHome, "systemd", "user", unit)
		content := fmt.Sprintf("[Unit]\nDescription=Coordica user Runner\nStartLimitIntervalSec=0\n\n[Service]\nType=simple\nExecStart=%s runner --data %s\nWorkingDirectory=%s\nEnvironment=%s\nRestart=on-failure\nRestartSec=5s\nKillMode=mixed\nTimeoutStopSec=15s\n\n[Install]\nWantedBy=default.target\n", systemdQuote(spec.Executable, true), systemdQuote(spec.DataDir, true), systemdQuote(spec.DataDir, false), systemdQuote("PATH="+r.Path, false))
		return plan{Name: unit, ConfigPath: path, Content: []byte(content), Preflight: cmd("--user", "show-environment"), Reload: cmd("--user", "daemon-reload"), Enable: cmd("--user", "enable", unit), Start: cmd("--user", "start", unit), Restart: cmd("--user", "restart", unit), Verify: cmd("--user", "is-active", "--quiet", unit)}, nil
	case "windows":
		// Task Scheduler expands environment references in ExecAction fields.
		// XML escaping cannot preserve a literal percent-containing path.
		if strings.Contains(spec.Executable, "%") || strings.Contains(spec.DataDir, "%") {
			return plan{}, errors.New("Windows service executable and data paths cannot contain percent signs; choose a path without percent signs")
		}
		if !strings.HasPrefix(r.SID, "S-1-") {
			return plan{}, errors.New("current Windows user SID is unavailable")
		}
		path := filepath.Join(spec.DataDir, "runner-task.xml")
		arguments := "runner --data " + windowsQuote(spec.DataDir)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
<RegistrationInfo><Description>Coordica current-user Runner</Description></RegistrationInfo>
<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId></LogonTrigger></Triggers>
<Principals><Principal id="CurrentUser"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><Enabled>true</Enabled><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure></Settings>
<Actions Context="CurrentUser"><Exec><Command>%s</Command><Arguments>%s</Arguments><WorkingDirectory>%s</WorkingDirectory></Exec></Actions>
</Task>
`, xmlString(r.SID), xmlString(r.SID), xmlString(spec.Executable), xmlString(arguments), xmlString(spec.DataDir))
		return plan{Name: name, ConfigPath: path, Content: windowsXML(content), Preflight: cmd("/?"), Enable: cmd("/Create", "/TN", name, "/XML", path, "/F"), Start: cmd("/Run", "/TN", name)}, nil
	}
	return plan{}, errors.New("unsupported user-service platform")
}

// systemd executes an argv, not a shell; it still expands % specifiers and $
// variables in ExecStart. Quote each argument and escape both explicitly.
func systemdQuote(text string, exec bool) string {
	text = strings.ReplaceAll(text, "%", "%%")
	if exec {
		text = strings.ReplaceAll(text, "$", "$$")
	}
	text = strings.ReplaceAll(text, "\\", "\\\\")
	text = strings.ReplaceAll(text, "\"", "\\\"")
	return "\"" + text + "\""
}

// windowsQuote follows the CommandLineToArgvW backslash/quote convention,
// including doubled trailing backslashes before a closing quote.
func windowsQuote(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, char := range text {
		if char == '\\' {
			slashes++
			continue
		}
		if char == '"' {
			b.WriteString(strings.Repeat("\\", slashes*2+1))
			b.WriteRune(char)
		} else {
			b.WriteString(strings.Repeat("\\", slashes))
			b.WriteRune(char)
		}
		slashes = 0
	}
	b.WriteString(strings.Repeat("\\", slashes*2))
	b.WriteByte('"')
	return b.String()
}
func windowsXML(text string) []byte {
	encoded := utf16.Encode([]rune(text))
	out := make([]byte, 2+2*len(encoded))
	out[0], out[1] = 0xff, 0xfe
	for i, v := range encoded {
		binary.LittleEndian.PutUint16(out[2+i*2:], v)
	}
	return out
}
