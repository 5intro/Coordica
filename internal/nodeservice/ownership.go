package nodeservice

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf16"
)

var errNotRegistered = errors.New("user service not registered")

type managerError struct {
	ExitCode int
	Output   string
}

func (e *managerError) Error() string {
	return fmt.Sprintf("user service manager rejected request (exit %d)", e.ExitCode)
}
func registrationAbsent(platform string, err error) bool {
	if errors.Is(err, errNotRegistered) {
		return true
	}
	var e *managerError
	if !errors.As(err, &e) {
		return false
	}
	text := strings.ToLower(e.Output)
	switch platform {
	case "darwin":
		return e.ExitCode == 113 && strings.Contains(text, "could not find service")
	case "linux":
		return (e.ExitCode == 1 || e.ExitCode == 4) && strings.Contains(e.Output, "LoadState=not-found") && !strings.Contains(e.Output, "LoadState=loaded")

	}
	return false
}
func fixedArgs(spec Spec) []string {
	if spec.CenterConfig != "" {
		return []string{spec.Executable, "center-serve", "--host-config", spec.CenterConfig}
	}
	return []string{spec.Executable, "runner", "--data", spec.DataDir}
}
func printedValue(v string) string {
	v = strings.TrimSpace(v)
	if s, e := strconv.Unquote(v); e == nil {
		return s
	}
	return v
}
func registeredMatches(spec Spec, info runtimeInfo, p plan, output string) bool {
	switch info.Platform {
	case "darwin":
		values := map[string]string{}
		args := []string{}
		inArgs := false
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if inArgs {
				if line == "}" {
					inArgs = false
				} else {
					args = append(args, printedValue(line))
				}
				continue
			}
			if line == "arguments = {" {
				inArgs = true
				continue
			}
			if key, value, ok := strings.Cut(line, " = "); ok && (key == "path" || key == "program") {
				if _, dup := values[key]; dup {
					return false
				}
				values[key] = printedValue(value)
			}
		}
		return !inArgs && values["path"] == p.ConfigPath && values["program"] == spec.Executable && reflect.DeepEqual(args, fixedArgs(spec))
	case "linux":
		values := map[string]string{}
		for _, line := range strings.Split(output, "\n") {
			k, v, ok := strings.Cut(line, "=")
			if ok {
				if _, dup := values[k]; dup {
					return false
				}
				values[k] = v
			}
		}
		if values["LoadState"] != "loaded" || values["FragmentPath"] != p.ConfigPath || values["DropInPaths"] != "" {
			return false
		}
		start := values["ExecStart"]
		if strings.Count(start, "{") != 1 || strings.Count(start, "path=") != 1 || strings.Count(start, "argv[]=") != 1 {
			return false
		}
		extract := func(key string) string {
			_, v, ok := strings.Cut(start, key)
			if !ok {
				return ""
			}
			v, _, ok = strings.Cut(v, " ; ")
			if !ok {
				return ""
			}
			// systemctl show prints these fields literally, not as quoted strings.
			return v
		}
		return extract("path=") == spec.Executable && extract("argv[]=") == strings.Join(fixedArgs(spec), " ")
	case "windows":
		text, ok := taskXMLText(output)
		if !ok {
			return false
		}
		var task struct {
			Principals []struct {
				UserID string `xml:"UserId"`
				Logon  string `xml:"LogonType"`
				Level  string `xml:"RunLevel"`
			} `xml:"Principals>Principal"`
			Actions struct {
				Exec []struct {
					Command string `xml:"Command"`
					Args    string `xml:"Arguments"`
					Dir     string `xml:"WorkingDirectory"`
				} `xml:"Exec"`
				Other   []struct{} `xml:"ComHandler"`
				Unknown []struct{} `xml:",any"`
			} `xml:"Actions"`
			Policy string `xml:"Settings>MultipleInstancesPolicy"`
		}
		if xml.Unmarshal([]byte(text), &task) != nil || len(task.Principals) != 1 || len(task.Actions.Exec) != 1 || len(task.Actions.Other) != 0 || len(task.Actions.Unknown) != 0 || task.Policy != "IgnoreNew" {
			return false
		}
		principal := task.Principals[0]
		action := task.Actions.Exec[0]
		var expected struct {
			Args string `xml:"Actions>Exec>Arguments"`
		}
		expectedText, ok := taskXMLText(string(p.Content))
		if !ok || xml.Unmarshal([]byte(expectedText), &expected) != nil {
			return false
		}
		return principal.UserID == info.SID && principal.Logon == "InteractiveToken" && principal.Level == "LeastPrivilege" && action.Command == spec.Executable && action.Args == expected.Args && action.Dir == spec.DataDir
	}
	return false
}
func taskXMLText(raw string) (string, bool) {
	b := []byte(raw)
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		if len(b)%2 != 0 {
			return "", false
		}
		wide := make([]uint16, (len(b)-2)/2)
		for i := range wide {
			wide[i] = binary.LittleEndian.Uint16(b[2+i*2:])
		}
		raw = string(utf16.Decode(wide))
	}
	raw = strings.TrimPrefix(raw, "\ufeff")
	raw = strings.ReplaceAll(raw, `encoding="UTF-16"`, `encoding="UTF-8"`)
	return raw, true
}
