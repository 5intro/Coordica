package process

import (
	"errors"
	"strings"
	"testing"
)

func TestBatchCommandQuotesOnlyRestrictedArguments(t *testing.T) {
	line, err := buildBatchCommand(`C:\Windows\System32\cmd.exe`, `C:\Users\开发者\App Data\npm\dsh.CMD`, []string{"--profile", "acp", "value with spaces", "", "Read,Glob,Grep", `C:\source\file.txt`})
	want := `"C:\Windows\System32\cmd.exe" /d /s /v:off /c ""C:\Users\开发者\App Data\npm\dsh.CMD" "--profile" "acp" "value with spaces" "" "Read,Glob,Grep" "C:\source\file.txt""`
	if err != nil || line != want {
		t.Fatalf("controlled command = %q, error=%v", line, err)
	}
}

func TestBatchCommandRejectsInjectionAndAmbiguousValues(t *testing.T) {
	interpreter, shim := `C:\Windows\System32\cmd.exe`, `C:\npm\tool.cmd`
	for _, value := range []string{`a"b`, "%PATH%", "!name!", "a^b", "a&b", "a|b", "a<b", "a>b", "a(b)", "line\r\n", "tab\t", "nul\x00", `C:\root\`, string([]byte{0xff})} {
		t.Run(strings.ReplaceAll(value, "\x00", "NUL"), func(t *testing.T) {
			if _, err := buildBatchCommand(interpreter, shim, []string{value}); !errors.Is(err, ErrUnsafeBatchCommand) {
				t.Fatalf("accepted unsafe argument %q: %v", value, err)
			}
		})
	}
	for _, badPath := range []string{`tool.cmd`, `C:tool.cmd`, `\server\share\tool.cmd`, `C:\Program Files (x86)\tool.cmd`, `C:\npm%\tool.cmd`, `C:\npm&\tool.cmd`, `C:\tool.cmd:stream`, `C:\tools\tool.exe`, `C:\tools\*.cmd`, "C:\\line\n\\tool.cmd"} {
		if _, err := buildBatchCommand(interpreter, badPath, nil); !errors.Is(err, ErrUnsafeBatchCommand) {
			t.Fatalf("accepted unsupported shim path %q: %v", badPath, err)
		}
	}
	if _, err := buildBatchCommand(`C:\fake.exe`, shim, nil); !errors.Is(err, ErrUnsafeBatchCommand) {
		t.Fatal("accepted non-system interpreter name", err)
	}
}

func TestBatchCommandRejectsUTF16LengthOverflow(t *testing.T) {
	if _, err := buildBatchCommand(`C:\Windows\System32\cmd.exe`, `C:\npm\tool.bat`, []string{strings.Repeat("😀", maxBatchCommandUnits/2)}); !errors.Is(err, ErrUnsafeBatchCommand) {
		t.Fatal("accepted command beyond cmd UTF16 limit", err)
	}
}
