package mcp

import (
	"context"
	"encoding/json"
	"io"
)

// Interaction combines local onboarding with optional limited online chat.
// Startup owns both providers; a Run server never constructs this dispatcher.
type Interaction struct {
	Bootstrap Bootstrap
	Chat      Chat
}

func (s Interaction) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	local, err := s.Bootstrap.provider()
	if err != nil {
		return err
	}
	return serve(ctx, in, out, true, local, optionalChat{s.Chat})
}

type optionalChat struct{ Chat }

func (s optionalChat) Tools(ctx context.Context) ([]any, error) {
	tools, err := s.Chat.Tools(ctx)
	if err != nil {
		return []any{}, nil
	}
	return tools, nil
}

// Cached or forged known chat calls always go through Chat.Call's identity
// check, even when optional discovery currently advertises no online tools.
func (s optionalChat) Call(ctx context.Context, name string, args json.RawMessage) (string, bool) {
	return s.Chat.Call(ctx, name, args)
}
