package nodeservice

import (
	"coordica/internal/jsoninput"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
)

// This script is product-owned and read-only. The only input is the fixed,
// generated task name, passed as data through the process environment. It
// connects locally as the current user and never registers or activates tasks.
const windowsTaskProbeScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$name = [Environment]::GetEnvironmentVariable('COORDICA_SERVICE_TASK_NAME', 'Process')
if ($name -cnotmatch '^coordica-(runner|center)-[0-9a-f]{16}$') { exit 2 }
$result = @{version = 1; task_name = $name; stage = 'connect'; hresult = 0; xml = ''}
try {
    $service = New-Object -ComObject 'Schedule.Service'
    $service.Connect()
    $result.stage = 'folder'
    $folder = $service.GetFolder('\')
    $result.stage = 'get_task'
    $task = $folder.GetTask($name)
    $result.stage = 'xml'
    $result.xml = [string]$task.Xml
    $result.stage = 'registered'
} catch {
    $failure = $_.Exception
    while ($null -ne $failure.InnerException) { $failure = $failure.InnerException }
    $result.hresult = ([int64]$failure.HResult -band 4294967295)
}
[Console]::Out.Write(($result | ConvertTo-Json -Compress))
exit 0
`

var windowsOwnedTaskName = regexp.MustCompile(`^coordica-(runner|center)-[0-9a-f]{16}$`)

func windowsTaskProbe(manager, name string) command {
	encoded := base64.StdEncoding.EncodeToString(windowsXML(windowsTaskProbeScript)[2:])
	return command{Program: filepath.Join(filepath.Dir(manager), "WindowsPowerShell", "v1.0", "powershell.exe"), Args: []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded}, WindowsTask: name}
}

func decodeWindowsTaskProbe(raw []byte, name string) (string, error) {
	var reply struct {
		Version  int     `json:"version"`
		TaskName string  `json:"task_name"`
		Stage    string  `json:"stage"`
		HRESULT  *uint32 `json:"hresult"`
		XML      *string `json:"xml"`
	}
	if !windowsOwnedTaskName.MatchString(name) || jsoninput.Decode(raw, &reply, 64<<10) != nil || reply.Version != 1 || reply.TaskName != name || reply.HRESULT == nil || reply.XML == nil {
		return "", errors.New("invalid native Task Scheduler probe response")
	}
	// HRESULT_FROM_WIN32(ERROR_FILE_NOT_FOUND) is absence only from GetTask,
	// never from Connect, opening the fixed root folder, or retrieving XML.
	if reply.Stage == "get_task" && *reply.HRESULT == 0x80070002 && *reply.XML == "" {
		return "", errNotRegistered
	}
	if reply.Stage != "registered" || *reply.HRESULT != 0 || *reply.XML == "" {
		return "", fmt.Errorf("native Task Scheduler probe refused (HRESULT 0x%08x)", *reply.HRESULT)
	}
	return *reply.XML, nil
}
