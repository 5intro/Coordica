package nodeconsole

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"coordica/internal/domain"
)

// localInfo provides a center shortcut only when the configured center is
// hosted on this computer. Older/offline centers keep the ordinary Runner page
// usable; this optional probe never exposes owner credentials or configuration.
func (s *Server) localInfo(w http.ResponseWriter, r *http.Request) {
	info := domain.LocalInfo{Mode: "runner"}
	center, valid := connectionURL(s.CenterURL)
	if !valid || s.API == nil || !centerOnThisMachine(center) {
		respond(w, http.StatusOK, info)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	data, err := s.API.Do(ctx, http.MethodGet, "/api/v1/runner/local-entry", "", nil)
	var entry domain.CenterEntry
	if err == nil && ctx.Err() == nil && len(data) <= 64<<10 && json.Unmarshal(data, &entry) == nil {
		advertised, matches := connectionURL(entry.RunnerURL)
		console, local := connectionURL(entry.ConsoleURL)
		if matches && sameConnection(center, advertised) && local && console.Scheme == "http" && net.ParseIP(console.Hostname()).IsLoopback() {
			info.CenterConsoleURL = strings.TrimRight(console.String(), "/")
		}
	}
	respond(w, http.StatusOK, info)
}

// connectionURL accepts an origin with a literal IP. No DNS lookup, credentials,
// path, query or fragment can influence the browser shortcut or center match.
func connectionURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "?#") || u.User != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || u.Path != "" && u.Path != "/" || u.RawPath != "" || u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil {
		return nil, false
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, false
		}
	}
	return u, true
}

func sameConnection(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && net.ParseIP(a.Hostname()).Equal(net.ParseIP(b.Hostname())) && connectionPort(a) == connectionPort(b)
}

func connectionPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		value, _ := strconv.Atoi(port)
		return strconv.Itoa(value)
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func centerOnThisMachine(center *url.URL) bool {
	ip := net.ParseIP(center.Hostname())
	if ip.IsLoopback() {
		return true
	}
	addresses, err := net.InterfaceAddrs()
	return err == nil && hasAddress(ip, addresses)
}

func hasAddress(ip net.IP, addresses []net.Addr) bool {
	for _, address := range addresses {
		switch address := address.(type) {
		case *net.IPNet:
			if ip.Equal(address.IP) {
				return true
			}
		case *net.IPAddr:
			if ip.Equal(address.IP) {
				return true
			}
		}
	}
	return false
}
