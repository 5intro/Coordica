package domain

// ConnectionInfo is the center's read-only runtime configuration for its local
// owner console. Only the public certificate is included, never private keys or
// owner/node credentials.
type ConnectionInfo struct {
	Platform          string `json:"platform,omitempty"`
	APIVersion        string `json:"api_version"`
	Version           string `json:"version"`
	RunnerListen      string `json:"runner_listen"`
	ConsoleListen     string `json:"console_listen"`
	ConsoleURL        string `json:"console_url"`
	RunnerURL         string `json:"runner_url"`
	RunnerConsoleURL  string `json:"runner_console_url"`
	DataDir           string `json:"data_dir"`
	CertificatePEM    string `json:"certificate_pem"`
	CertificateSHA256 string `json:"certificate_sha256"`
}

// CenterEntry gives a node only the addresses needed to recognize a center
// hosted on the same computer. It contains no owner configuration or secrets.
type CenterEntry struct {
	ConsoleURL string `json:"console_url"`
	RunnerURL  string `json:"runner_url"`
}

type LocalInfo struct {
	Mode             string `json:"mode"`
	CenterConsoleURL string `json:"center_console_url,omitempty"`
}
