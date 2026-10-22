package httpapi

import "net/http"

// The loopback-only extension point keeps workspace-specific transports out
// of the legacy dispatcher. Each workspace still calls shared app use cases.
func registerAdminWorkspaces(mux *http.ServeMux, s *Server) {
	registerLeaderActivity(mux, s)
	registerTestCatalog(mux, s)
}
