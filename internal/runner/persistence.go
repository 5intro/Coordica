package runner

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"coordica/internal/domain"
	"coordica/internal/localfile"
)

type spool struct {
	Assignment   domain.Assignment       `json:"assignment"`
	Key          string                  `json:"key"`
	PID          int                     `json:"pid"`
	WorkDir      string                  `json:"work_dir"`
	StartedAt    time.Time               `json:"started_at"`
	Request      *domain.CompleteRequest `json:"request,omitempty"`
	Intent       *intentHandoff          `json:"intent,omitempty"`
	Acknowledged bool                    `json:"acknowledged"`
}
type claimPending struct {
	Key     string              `json:"key"`
	Request domain.ClaimRequest `json:"request"`
}

func randomKey() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}
func runName(id string) string {
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:16])
}
func (r *Runner) spoolPath(id string) string {
	return filepath.Join(r.config.DataDir, "spool", runName(id)+".json")
}

func writePrivateJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return localfile.WritePrivate(path, raw)
}
func readJSON(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) > 8<<20 {
		return errors.New("persisted record exceeds limit")
	}
	return json.Unmarshal(raw, out)
}

// Load restores node identity and TLS settings from a previously paired Runner.
func Load(dataDir string) (Config, error) {
	var config Config
	err := readJSON(filepath.Join(dataDir, "runner.json"), &config)
	config.DataDir = dataDir
	return config, err
}
