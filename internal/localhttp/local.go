// Package localhttp defines the transport boundary for machine-local consoles.
// It does not authenticate users or authorize application operations.
package localhttp

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

// Address permits only a literal loopback address; localhost is normalized to
// IPv4 so the binding never depends on DNS or an OS's dual-stack defaults.
func Address(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return "", errors.New("console address must include a loopback host and port")
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("console must listen on a literal loopback address, such as 127.0.0.1:8080")
	}
	return net.JoinHostPort(host, port), nil
}

// Guard checks the actual socket peer and request authority. Forwarding headers
// never turn a remote connection into a local one. The Host check also rejects
// nonlocal DNS names that resolve to loopback.
func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(peer)
		host, _, hostErr := net.SplitHostPort(r.Host)
		if hostErr != nil {
			host = r.Host
			if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
				host = host[1 : len(host)-1]
			}
		}
		hostIP := net.ParseIP(host)
		localHost := strings.EqualFold(host, "localhost") || hostIP != nil && hostIP.IsLoopback()
		origin := r.Header.Get("Origin")
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if err != nil || ip == nil || !ip.IsLoopback() || !localHost || origin != "" && origin != scheme+"://"+r.Host {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"forbidden","message":"控制台仅允许本机同源访问"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
