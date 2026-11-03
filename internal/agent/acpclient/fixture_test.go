package acpclient

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The child is a hand-written ACP simulation, never an installed harness.
func TestMain(m *testing.M) {
	if os.Getenv("COORDICA_ACP_FIXTURE") != "1" {
		os.Exit(m.Run())
	}
	if len(os.Args) != 2 || os.Args[1] != "--offline-acp" || os.Getenv("ACP_EXPLICIT_ENV") != "selected" || os.Getenv("ACP_PARENT_FORBIDDEN") != "" {
		os.Exit(2)
	}
	scenario := os.Getenv("COORDICA_ACP_SCENARIO")
	if scenario == "no-stdin" {
		time.Sleep(30 * time.Second)
		os.Exit(3)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), maxFrameBytes)
	for scanner.Scan() {
		f, err := os.OpenFile(os.Getenv("COORDICA_ACP_JOURNAL"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(4)
		}
		_, err = f.Write(append(append([]byte(nil), scanner.Bytes()...), '\n'))
		f.Close()
		if err != nil {
			os.Exit(5)
		}
		var request struct {
			JSONRPC string
			ID      int
			Method  string
			Params  struct {
				ProtocolVersion    int
				ClientCapabilities struct {
					FS       struct{ ReadTextFile, WriteTextFile bool }
					Terminal bool
				}
			}
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.JSONRPC != "2.0" || request.ID != 1 || request.Method != "initialize" || request.Params.ProtocolVersion != 1 ||
			request.Params.ClientCapabilities.Terminal || request.Params.ClientCapabilities.FS.ReadTextFile || request.Params.ClientCapabilities.FS.WriteTextFile {
			os.Exit(6)
		}
		if scenario == "disconnect" {
			os.Exit(0)
		}
		if scenario == "no-response" {
			continue
		}
		result := map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "offline-acp", "version": "fixture"},
			"agentCapabilities": map[string]any{"loadSession": true, "promptCapabilities": map[string]bool{"image": true}, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"_meta": nil}},
			"authMethods":       []any{map[string]string{"id": "offline-auth", "name": "Offline method"}}}
		if scenario == "minimal" {
			result = map[string]any{"protocolVersion": 1, "agentInfo": nil}
		}
		switch scenario {
		case "bad-version":
			result["protocolVersion"] = 2
		case "null-caps":
			result["agentCapabilities"] = nil
		case "null-auth":
			result["authMethods"] = nil
		case "null-bool":
			result["agentCapabilities"] = map[string]any{"loadSession": nil}
		}
		if scenario == "stderr" {
			fmt.Fprintln(os.Stderr, "private-stderr-sentinel")
		}
		response := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
		switch scenario {
		case "wrong-id":
			response["id"] = "1"
		case "both":
			response["error"] = map[string]any{"code": -32000, "message": "private-acp-rejection"}
		case "result-array":
			response["result"] = []any{}
		case "rejected":
			delete(response, "result")
			response["error"] = map[string]any{"code": -32000, "message": "private-acp-rejection", "data": map[string]string{"private": "private-acp-rejection"}}
		case "callback":
			response = map[string]any{"jsonrpc": "2.0", "id": 17, "method": "session/request_permission", "params": map[string]any{"private": "private-acp-callback"}}
		case "duplicate-fields":
			fmt.Println(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"protocolVersion":2}}`)
			continue
		case "stdout-noise":
			fmt.Println("private-not-json")
			continue
		case "oversize":
			fmt.Println(strings.Repeat("X", maxFrameBytes+1))
			continue
		}
		raw, _ := json.Marshal(response)
		fmt.Println(string(raw))
		if scenario == "duplicate-response" {
			fmt.Println(string(raw))
		}
		if scenario == "rejected" {
			os.Exit(0)
		}
		if scenario == "post-callback" {
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(os.Getenv("COORDICA_ACP_CONTINUE")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(7)
				}
				time.Sleep(5 * time.Millisecond)
			}
			fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"private":"private-acp-callback"}}`)
		}
	}
	os.Exit(0)
}

func fixtureConfig(t *testing.T, scenario string) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"COORDICA_ACP_FIXTURE=1", "COORDICA_ACP_SCENARIO=" + scenario, "COORDICA_ACP_JOURNAL=" + journal, "COORDICA_ACP_CONTINUE=" + filepath.Join(dir, "continue"), "ACP_EXPLICIT_ENV=selected", "GOMAXPROCS=1", "GORACE=atexit_sleep_ms=0"}
	for _, name := range []string{"PATH", "SystemRoot", "TMPDIR", "TMP", "TEMP"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return Config{Executable: executable, Args: []string{"--offline-acp"}, Directory: dir, Env: env}, journal
}
