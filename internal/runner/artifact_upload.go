package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"coordica/internal/domain"
)

const maxArtifactBytes = 64 << 20

// This is a bounded live handoff, not restart recovery. Freeze material once;
// retries cannot reopen a changed pathname or execute the Agent again.
func uploadArtifact(ctx context.Context, r *Runner, runID, key, path string, permissionUntil time.Time) (domain.Artifact, error) {
	var artifact domain.Artifact
	until := time.Now().Add(5 * time.Second)
	if permissionUntil.Before(until) {
		until = permissionUntil
	}
	ctx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	material, digest, err := artifactMaterial(ctx, path)
	if err != nil {
		return artifact, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return artifact, err
		}
		artifact = domain.Artifact{}
		// Keep GetBody unset: the HTTP transport must not add hidden POST
		// replays beyond this loop's three explicit attempts.
		body := io.NopCloser(bytes.NewReader(material))
		_, err = r.request(ctx, "POST", "/api/v1/runs/"+runID+"/artifacts", key, body, "application/octet-stream", &artifact)
		if err == nil {
			if artifact.ID != runID+"_"+digest || artifact.RunID != runID ||
				artifact.SHA256 != digest || artifact.Size != int64(len(material)) {
				return domain.Artifact{}, errors.New("artifact acknowledgement does not match fixed material")
			}
			return artifact, nil
		}
		if attempt == 2 || !retryArtifactUpload(err) {
			return domain.Artifact{}, err
		}
		delay := []time.Duration{200 * time.Millisecond, 800 * time.Millisecond}[attempt]
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return domain.Artifact{}, ctx.Err()
		case <-timer.C:
		}
	}
	return domain.Artifact{}, err
}

func artifactMaterial(ctx context.Context, path string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxArtifactBytes {
		return nil, "", errors.New("artifact must be a nonempty regular file within 64 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, "", errors.New("artifact file identity changed before reading")
	}
	material, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, "", err
	}
	after, err := file.Stat()
	if err != nil || int64(len(material)) != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		return nil, "", errors.New("artifact changed while reading fixed material")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(material)
	return material, hex.EncodeToString(digest[:]), nil
}

func retryArtifactUpload(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var response *apiError
	if errors.As(err, &response) {
		return response.Status == 502 || response.Status == 503 || response.Status == 504
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var temporary net.Error
	return errors.As(err, &temporary) && (temporary.Timeout() || temporary.Temporary())
}
