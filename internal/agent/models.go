package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"coordica/internal/process"
)

// Tool-owned metadata. It deliberately contains no scheduling or domain state.
type ModelOption struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	ReasoningEfforts       []string `json:"reasoning_efforts"`
	DefaultReasoningEffort string   `json:"default_reasoning_effort,omitempty"`
}

type modelCacheEntry struct {
	models []ModelOption
	status string
	until  time.Time
}

func cloneModels(models []ModelOption) []ModelOption {
	copy := append([]ModelOption(nil), models...)
	for i := range copy {
		copy[i].ReasoningEfforts = append([]string(nil), copy[i].ReasoningEfforts...)
	}
	return copy
}

// Refresh is demand-driven and bounded. Idle claim polling reuses the report;
// failures never reuse an earlier successful catalog or disable the whole tool.
func (r *Registry) discoverModels(ctx context.Context, def Definition, cap Capability) ([]ModelOption, string) {
	info, err := os.Stat(cap.Executable)
	if err != nil {
		return nil, "unavailable"
	}
	key := def.ID + "\x00" + cap.Executable + "\x00" + cap.Version + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" + info.ModTime().String()
	for {
		if ctx.Err() != nil {
			return nil, "unavailable"
		}
		r.modelMu.Lock()
		if cached, ok := r.modelCache[key]; ok && time.Now().Before(cached.until) {
			r.modelMu.Unlock()
			return cloneModels(cached.models), cached.status
		}
		if pending := r.modelReads[key]; pending != nil {
			r.modelMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, "unavailable"
			case <-pending:
				continue
			}
		}
		if r.modelReads == nil {
			r.modelReads = map[string]chan struct{}{}
		}
		r.modelReads[key] = make(chan struct{})
		r.modelMu.Unlock()
		break
	}
	defer func() {
		r.modelMu.Lock()
		close(r.modelReads[key])
		delete(r.modelReads, key)
		r.modelMu.Unlock()
	}()
	// Only callers for this exact catalog wait. Never hold the cache mutex
	// during process/network I/O or while waiting for another caller.
	models, err := readToolModels(ctx, def, cap.Executable)
	status, ttl := "reported", 5*time.Minute
	if err != nil {
		models, status, ttl = nil, "unavailable", 30*time.Second
	}
	if ctx.Err() != nil {
		return nil, "unavailable"
	}
	r.modelMu.Lock()
	if r.modelCache == nil || len(r.modelCache) >= 64 {
		r.modelCache = map[string]modelCacheEntry{}
	}
	r.modelCache[key] = modelCacheEntry{cloneModels(models), status, time.Now().Add(ttl)}
	r.modelMu.Unlock()
	return models, status
}

func (r *Registry) validateExecutionOptions(ctx context.Context, def Definition, req Request) error {
	// launchArgs supplies this same default for implicit Leader/initialization
	// requests. Validate the effective model even when no Runner selected it.
	if def.Protocol == ProtocolCodex && req.Model == "" {
		req.Model = "gpt-6.1-sol"
	}
	if req.Model == "" && req.ReasoningEffort == "" {
		return nil
	}
	if def.Protocol == ProtocolACP || def.Protocol == ProtocolPi {
		// The actual execution session verifies configuration before prompting.
		if !validModelID(req.Model) || req.ReasoningEffort != "" && !effortID.MatchString(req.ReasoningEffort) {
			return ErrProtocol
		}
		if def.Protocol == ProtocolPi {
			_, _, err := splitPiModel(req.Model)
			return err
		}
		return nil
	}
	models, err := readToolModels(ctx, def, req.Executable)
	if err != nil {
		// Older CLIs without catalogs retain only the explicit legacy default.
		// A successful current catalog, even an empty one, is authoritative.
		if def.Protocol == ProtocolCodex && req.Model == "gpt-6.1-sol" && req.ReasoningEffort == "" && ctx.Err() == nil && !errors.Is(err, process.ErrTreeNotStopped) {
			return nil
		}
		return fmt.Errorf("cannot verify execution model options: %w", err)
	}
	if modelSupported(models, req.Model, req.ReasoningEffort) {
		return nil
	}
	return errors.New("execution model or reasoning effort is no longer supported")
}

// ACP model values can be opaque strings such as JSON-encoded provider/model
// pairs. They remain one structured value, never shell text or extra argv.
func validModelID(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && !strings.HasPrefix(value, "-") && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

var modelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,127}$`)
var effortID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,63}$`)

// Only initialize and model/list are sent: discovery never starts a thread,
// runs a prompt, changes user settings, or accepts a server tool request.
func codexModels(parent context.Context, executable string) (catalog []ModelOption, catalogErr error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	peer, err := startRPCSpec(process.Spec{Executable: executable, Args: []string{"app-server", "--listen", "stdio://"}, Dir: os.TempDir(), Env: developmentEnv(), MaxLineBytes: 1 << 20}, nil)
	if err != nil {
		return nil, err
	}
	defer peer.stopWriter()
	defer func() {
		peer.process.Stop()
		for range peer.process.Output() {
		}
		status, waitErr := peer.process.Wait()
		if !status.TreeStopped {
			catalog = nil
			catalogErr = errors.Join(catalogErr, waitErr, process.ErrTreeNotStopped)
		}
	}()
	request := func(id int, method string, params any) error {
		return peer.send(map[string]any{"id": id, "method": method, "params": params})
	}
	if err := request(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "coordica_catalog", "version": "1"}}); err != nil {
		return nil, err
	}
	id, bytes, messages := 1, 0, 0
	var models []ModelOption
	seen, cursors := map[string]bool{}, map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-peer.writeErrors:
			return nil, err
		case output, ok := <-peer.process.Output():
			if !ok {
				return nil, ErrProtocol
			}
			bytes += len(output.Line)
			messages++
			if bytes > 2<<20 || messages > 256 {
				return nil, ErrProtocol
			}
			if output.Stream != "stdout" {
				continue
			}
			var response struct {
				ID     int             `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(output.Line, &response) != nil {
				return nil, ErrProtocol
			}
			if response.Method != "" {
				if response.ID != 0 {
					return nil, ErrProtocol
				}
				continue
			}
			if response.ID != id || len(response.Error) != 0 && string(response.Error) != "null" || len(response.Result) == 0 {
				return nil, ErrProtocol
			}
			if id == 1 {
				if err := peer.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
					return nil, err
				}
			} else {
				var page struct {
					Data []struct {
						Model   string `json:"model"`
						Name    string `json:"displayName"`
						Hidden  bool   `json:"hidden"`
						Default string `json:"defaultReasoningEffort"`
						Efforts []struct {
							Value string `json:"reasoningEffort"`
						} `json:"supportedReasoningEfforts"`
					} `json:"data"`
					Cursor *string `json:"nextCursor"`
				}
				if json.Unmarshal(response.Result, &page) != nil || page.Data == nil || len(page.Data) > 64 {
					return nil, ErrProtocol
				}
				for _, raw := range page.Data {
					if raw.Hidden {
						continue
					}
					if !modelID.MatchString(raw.Model) || seen[raw.Model] || len(raw.Name) > 256 || len(raw.Efforts) > 16 {
						return nil, ErrProtocol
					}
					seen[raw.Model] = true
					option := ModelOption{ID: raw.Model, Name: raw.Name, DefaultReasoningEffort: raw.Default, ReasoningEfforts: []string{}}
					efforts := map[string]bool{}
					for _, effort := range raw.Efforts {
						if !effortID.MatchString(effort.Value) || efforts[effort.Value] {
							return nil, ErrProtocol
						}
						efforts[effort.Value] = true
						option.ReasoningEfforts = append(option.ReasoningEfforts, effort.Value)
					}
					if raw.Default != "" && !efforts[raw.Default] {
						return nil, ErrProtocol
					}
					models = append(models, option)
				}
				if len(models) > 64 {
					return nil, ErrProtocol
				}
				if page.Cursor == nil || *page.Cursor == "" {
					return models, nil
				}
				if len(*page.Cursor) > 1024 || cursors[*page.Cursor] || id >= 8 {
					return nil, ErrProtocol
				}
				cursors[*page.Cursor] = true
				id++
				if err := request(id, "model/list", map[string]any{"limit": 64, "includeHidden": false, "cursor": *page.Cursor}); err != nil {
					return nil, err
				}
				continue
			}
			id++
			if err := request(id, "model/list", map[string]any{"limit": 64, "includeHidden": false}); err != nil {
				return nil, err
			}
		}
	}
}
