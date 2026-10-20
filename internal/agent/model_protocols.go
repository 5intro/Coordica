package agent

import (
	"bytes"
	"context"
	"coordica/internal/process"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// A metadata connection never sends a prompt or accepts server tool requests.
// Every response is bounded and the entire supervised tree is stopped on exit.
type metadataPeer struct {
	ctx              context.Context
	cancel           context.CancelFunc
	rpc              *rpcProcess
	dir              string
	tempDir          string
	count, bytes, id int
}

func newMetadataPeer(parent context.Context, executable string, args []string) (*metadataPeer, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	dir, err := os.MkdirTemp("", "coordica-catalog-")
	if err != nil {
		cancel()
		return nil, err
	}
	rpc, err := startRPCSpec(process.Spec{Executable: executable, Args: args, Dir: dir, Env: developmentEnv(), MaxLineBytes: 1 << 20}, nil)
	if err != nil {
		cancel()
		os.RemoveAll(dir)
		return nil, err
	}
	return &metadataPeer{ctx: ctx, cancel: cancel, rpc: rpc, dir: dir, tempDir: dir}, nil
}
func (p *metadataPeer) close() error {
	defer p.cancel()
	defer p.rpc.stopWriter()
	defer os.RemoveAll(p.tempDir)
	p.rpc.process.Stop()
	for range p.rpc.process.Output() {
	}
	status, err := p.rpc.process.Wait()
	if !status.TreeStopped {
		return errors.Join(err, process.ErrTreeNotStopped)
	}
	return nil
}
func (p *metadataPeer) read() (json.RawMessage, error) {
	for {
		select {
		case <-p.ctx.Done():
			return nil, p.ctx.Err()
		case err := <-p.rpc.writeErrors:
			return nil, err
		case line, ok := <-p.rpc.process.Output():
			if !ok {
				return nil, ErrProtocol
			}
			p.count++
			p.bytes += len(line.Line)
			if p.count > 256 || p.bytes > 2<<20 {
				return nil, ErrProtocol
			}
			if line.Stream != "stdout" {
				continue
			}
			if !json.Valid(line.Line) {
				// Some CLI profiles emit plain diagnostics on stdout before a
				// JSON-RPC response. Ignore only bounded non-object noise; no
				// facts are parsed from it. A malformed protocol object fails.
				if raw := bytes.TrimSpace(line.Line); len(raw) == 0 || raw[0] != '{' {
					continue
				}
				return nil, ErrProtocol
			}
			return json.RawMessage(line.Line), nil
		}
	}
}

// A rejected call is known only after validating the response envelope and
// request ID. Lost, malformed or mismatched replies remain uncertain.
type metadataRejection struct{ cause error }

func (e *metadataRejection) Error() string { return e.cause.Error() }
func (e *metadataRejection) Unwrap() error { return e.cause }

func (p *metadataPeer) call(method string, params any) (json.RawMessage, error) {
	p.id++
	if err := p.rpc.send(map[string]any{"jsonrpc": "2.0", "id": p.id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		raw, err := p.read()
		if err != nil {
			return nil, err
		}
		var reply struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Method  string          `json:"method"`
			Result  json.RawMessage `json:"result"`
			Error   json.RawMessage `json:"error"`
		}
		if json.Unmarshal(raw, &reply) != nil {
			return nil, ErrProtocol
		}
		if reply.Method != "" {
			if reply.ID != 0 {
				return nil, ErrProtocol
			}
			continue
		}
		if reply.JSONRPC != "2.0" || reply.ID != p.id {
			return nil, ErrProtocol
		}
		if len(reply.Error) > 0 && string(reply.Error) != "null" {
			var failure struct {
				Code    *int    `json:"code"`
				Message *string `json:"message"`
			}
			if len(reply.Result) != 0 || json.Unmarshal(reply.Error, &failure) != nil || failure.Code == nil || failure.Message == nil {
				return nil, ErrProtocol
			}
			return nil, &metadataRejection{cause: fmt.Errorf("metadata %s: %w", method, rpcError(reply.Error))}
		}
		if len(reply.Result) == 0 {
			return nil, fmt.Errorf("%w: metadata %s reply", ErrProtocol, method)
		}
		return reply.Result, nil
	}
}
func (p *metadataPeer) command(command string, fields map[string]any) (json.RawMessage, error) {
	p.id++
	id := fmt.Sprintf("catalog-%d", p.id)
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"], fields["id"] = command, id
	if err := p.rpc.send(fields); err != nil {
		return nil, err
	}
	for {
		raw, err := p.read()
		if err != nil {
			return nil, err
		}
		var reply struct {
			Type, ID, Command string
			Success           bool
			Data              json.RawMessage
		}
		if json.Unmarshal(raw, &reply) != nil {
			return nil, ErrProtocol
		}
		if reply.Type != "response" {
			if reply.Type == "extension_ui_request" {
				return nil, ErrProtocol
			}
			continue
		}
		if reply.ID != id || reply.Command != command || !reply.Success {
			return nil, ErrProtocol
		}
		return reply.Data, nil
	}
}
func validateModels(models []ModelOption) error {
	if len(models) == 0 || len(models) > 64 {
		return ErrProtocol
	}
	seen := map[string]bool{}
	for _, model := range models {
		if !validModelID(model.ID) || seen[model.ID] || len(model.Name) > 256 || len(model.ReasoningEfforts) > 16 {
			return ErrProtocol
		}
		seen[model.ID] = true
		efforts := map[string]bool{}
		for _, effort := range model.ReasoningEfforts {
			if !effortID.MatchString(effort) || efforts[effort] {
				return ErrProtocol
			}
			efforts[effort] = true
		}
		if model.DefaultReasoningEffort != "" && !efforts[model.DefaultReasoningEffort] {
			return ErrProtocol
		}
	}
	return nil
}

type acpConfigValue struct {
	Value, Name string
	Options     []acpConfigValue
}
type acpConfigOption struct {
	ID, Name, Category, Type, CurrentValue string
	Options                                []acpConfigValue
}

func (o *acpConfigOption) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID, Name, Category, Type string
		CurrentValue             json.RawMessage
		Options                  json.RawMessage
	}
	if json.Unmarshal(data, &raw) != nil {
		return ErrProtocol
	}
	*o = acpConfigOption{ID: raw.ID, Name: raw.Name, Category: raw.Category, Type: raw.Type}
	// Unknown controls retain their agent defaults. Only select values enter
	// model discovery or configuration; unrelated future types are ignored.
	if raw.Type != "select" {
		return nil
	}
	if json.Unmarshal(raw.CurrentValue, &o.CurrentValue) != nil || json.Unmarshal(raw.Options, &o.Options) != nil {
		return ErrProtocol
	}
	return nil
}

func configValues(options []acpConfigValue) []acpConfigValue {
	var values []acpConfigValue
	for _, option := range options {
		if option.Value != "" {
			values = append(values, option)
		} else {
			for _, child := range option.Options {
				if child.Value != "" {
					values = append(values, child)
				}
			}
		}
	}
	return values
}
func configOption(options []acpConfigOption, category string) *acpConfigOption {
	for i := range options {
		if options[i].Category == category && options[i].Type == "select" {
			return &options[i]
		}
	}
	return nil
}
func configContains(option *acpConfigOption, value string) bool {
	if option == nil {
		return false
	}
	for _, v := range configValues(option.Options) {
		if v.Value == value {
			return true
		}
	}
	return false
}
func acpModels(ctx context.Context, def Definition, executable string) (models []ModelOption, err error) {
	// Bound lock contention and protocol discovery together. A healthy peer
	// normally releases its probe inside the per-connection metadata timeout.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if !def.Resume || def.ResumeMethod == "" {
		return nil, errors.New("ACP catalog requires reusable sessions")
	}
	owned, unlock, err := lockCatalogSession(ctx, def, executable)
	if err != nil {
		return nil, err
	}
	defer unlock()
	p, err := newMetadataPeer(ctx, executable, def.StartArgs)
	if err != nil {
		return nil, err
	}
	p.dir = owned.workDir
	sessionID := ""
	canClose := false
	defer func() {
		if sessionID != "" && canClose && def.CloseMethod != "" {
			_, closeErr := p.call(def.CloseMethod, map[string]string{"sessionId": sessionID})
			err = errors.Join(err, closeErr)
		}
		err = errors.Join(err, p.close())
		if err != nil {
			models = nil
		}
	}()
	raw, err := p.call("initialize", map[string]any{"protocolVersion": 1, "clientInfo": map[string]string{"name": "coordica_catalog", "version": "1"}, "clientCapabilities": map[string]any{}})
	if err != nil {
		return nil, err
	}
	var init struct {
		AgentCapabilities struct {
			LoadSession bool                       `json:"loadSession"`
			Sessions    map[string]json.RawMessage `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
		ProtocolVersion int
		AuthMethods     []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if json.Unmarshal(raw, &init) != nil || init.ProtocolVersion != 1 {
		return nil, ErrProtocol
	}
	_, canClose = init.AgentCapabilities.Sessions["close"]
	_, canResume := init.AgentCapabilities.Sessions["resume"]
	if def.ResumeMethod == "session/load" {
		canResume = init.AgentCapabilities.LoadSession
	}
	if !canResume {
		return nil, errors.New("ACP peer does not advertise reusable catalog sessions")
	}
	if def.AuthMethod != "" {
		advertised := false
		for _, method := range init.AuthMethods {
			advertised = advertised || method.ID == def.AuthMethod
		}
		if !advertised {
			return nil, errors.New("ACP peer does not advertise registered authentication method")
		}
		if _, err = p.call("authenticate", map[string]string{"methodId": def.AuthMethod}); err != nil {
			return nil, err
		}
	}
	params := map[string]any{"cwd": p.dir, "mcpServers": []any{}}
	method := "session/new"
	if owned.SessionID != "" {
		method = def.ResumeMethod
		params["sessionId"] = owned.SessionID
	} else {
		// Persist before creating: a lost reply/crash must not cause the next
		// poll to create another session with an unknown previous outcome.
		owned.Pending, owned.Retryable = true, false
		if err := owned.save(); err != nil {
			return nil, err
		}
	}
	raw, err = p.call(method, params)
	if err != nil {
		var rejection *metadataRejection
		if method == "session/new" && errors.As(err, &rejection) {
			owned.Pending, owned.Retryable = false, true
			err = errors.Join(err, owned.save())
		}
		return nil, err
	}
	var session struct {
		SessionID     string
		ConfigOptions []acpConfigOption
	}
	if json.Unmarshal(raw, &session) != nil {
		return nil, ErrProtocol
	}
	if method != "session/new" {
		sessionID = owned.SessionID
		if session.SessionID != "" && session.SessionID != sessionID {
			return nil, ErrProtocol
		}
	} else {
		if session.SessionID == "" || len(session.SessionID) > 512 {
			return nil, ErrProtocol
		}
		sessionID = session.SessionID
		owned.SessionID, owned.Pending, owned.Retryable = sessionID, false, false
		if err := owned.save(); err != nil {
			return nil, err
		}
	}
	model := configOption(session.ConfigOptions, "model")
	if model == nil {
		return nil, ErrProtocol
	}
	thought := configOption(session.ConfigOptions, "thought_level")
	for _, value := range configValues(model.Options) {
		option := ModelOption{ID: value.Value, Name: value.Name, ReasoningEfforts: []string{}}
		// Other models can have different effort menus. Only report effort values
		// returned for the current model; do not mutate defaults during discovery.
		if value.Value == model.CurrentValue && thought != nil {
			for _, level := range configValues(thought.Options) {
				option.ReasoningEfforts = append(option.ReasoningEfforts, level.Value)
			}
			option.DefaultReasoningEffort = thought.CurrentValue
		}
		models = append(models, option)
	}
	return models, validateModels(models)
}
func piModels(ctx context.Context, def Definition, executable string) (models []ModelOption, err error) {
	args := append(append([]string{}, def.StartArgs...), "--no-session", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-mcp")
	p, err := newMetadataPeer(ctx, executable, args)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, p.close())
		if err != nil {
			models = nil
		}
	}()
	raw, err := p.command("get_available_models", nil)
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Models []struct{ ID, Provider, Name string }
	}
	if json.Unmarshal(raw, &catalog) != nil {
		return nil, ErrProtocol
	}
	raw, err = p.command("get_state", nil)
	if err != nil {
		return nil, err
	}
	var state struct {
		Model         *struct{ ID, Provider string }
		ThinkingLevel string
	}
	if json.Unmarshal(raw, &state) != nil {
		return nil, ErrProtocol
	}
	raw, err = p.command("get_available_thinking_levels", nil)
	if err != nil {
		return nil, err
	}
	var levels struct{ Levels []string }
	if json.Unmarshal(raw, &levels) != nil {
		return nil, ErrProtocol
	}
	for _, m := range catalog.Models {
		option := ModelOption{ID: m.Provider + "/" + m.ID, Name: m.Name, ReasoningEfforts: []string{}}
		// Pi executes separate provider/model fields. Validate their exact
		// round trip here rather than advertising values execution rejects.
		provider, id, parseErr := splitPiModel(option.ID)
		if parseErr != nil || provider != m.Provider || id != m.ID {
			return nil, ErrProtocol
		}
		if state.Model != nil && state.Model.ID == m.ID && state.Model.Provider == m.Provider {
			option.ReasoningEfforts = levels.Levels
			option.DefaultReasoningEffort = state.ThinkingLevel
		}
		models = append(models, option)
	}
	return models, validateModels(models)
}
func claudeModels(ctx context.Context, executable string) (models []ModelOption, err error) {
	p, err := newMetadataPeer(ctx, executable, []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--no-session-persistence", "--setting-sources", ""})
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, p.close())
		if err != nil {
			models = nil
		}
	}()
	if err = p.rpc.send(map[string]any{"type": "control_request", "request_id": "catalog-1", "request": map[string]any{"subtype": "initialize", "hooks": map[string]any{}}}); err != nil {
		return nil, err
	}
	for {
		raw, e := p.read()
		if e != nil {
			return nil, e
		}
		var reply struct {
			Type     string
			Response struct {
				Subtype   string
				RequestID string `json:"request_id"`
				Response  struct {
					Models []struct {
						Value                 string
						DisplayName           string
						SupportedEffortLevels []string
					}
				}
			}
		}
		if json.Unmarshal(raw, &reply) != nil {
			return nil, ErrProtocol
		}
		if reply.Type == "control_request" {
			return nil, ErrProtocol
		}
		if reply.Type != "control_response" {
			continue
		}
		if reply.Response.RequestID != "catalog-1" || reply.Response.Subtype != "success" {
			return nil, ErrProtocol
		}
		for _, m := range reply.Response.Response.Models {
			models = append(models, ModelOption{ID: m.Value, Name: m.DisplayName, ReasoningEfforts: append([]string{}, m.SupportedEffortLevels...)})
		}
		return models, validateModels(models)
	}
}
func readToolModels(ctx context.Context, def Definition, executable string) ([]ModelOption, error) {
	switch def.Protocol {
	case ProtocolCodex:
		return codexModels(ctx, executable)
	case ProtocolACP:
		return acpModels(ctx, def, executable)
	case ProtocolPi:
		return piModels(ctx, def, executable)
	case ProtocolClaude:
		return claudeModels(ctx, executable)
	}
	return nil, ErrProtocol
}
func modelSupported(models []ModelOption, model, effort string) bool {
	for _, option := range models {
		if option.ID != model {
			continue
		}
		if effort == "" {
			return true
		}
		for _, level := range option.ReasoningEfforts {
			if level == effort {
				return true
			}
		}
	}
	return false
}
func splitPiModel(value string) (string, string, error) {
	provider, id, ok := strings.Cut(value, "/")
	if !ok || !modelID.MatchString(provider) || !modelID.MatchString(id) {
		return "", "", ErrProtocol
	}
	return provider, id, nil
}
