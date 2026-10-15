package runner

import (
	"errors"
	"net/url"
	"strings"
)

// A private machine-local runtime option, never supplied by a task/assignment.
// Credentials belong to official model login, not persisted proxy URLs.
func ValidateHTTPSProxy(raw string) error {
	if raw == "" {
		return nil
	}
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || strings.ContainsAny(raw, "\x00\r\n") {
		return errors.New("Runner HTTPS proxy must be a fixed non-secret HTTP(S) origin")
	}
	return nil
}
