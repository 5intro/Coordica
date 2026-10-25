package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"coordica/internal/domain"
)

func artifactTestReceipt(body []byte) domain.Artifact {
	digest := sha256.Sum256(body)
	sum := hex.EncodeToString(digest[:])
	return domain.Artifact{ID: "run_test_" + sum, RunID: "run_test", SHA256: sum, Size: int64(len(body))}
}

func artifactUploadTest(t *testing.T, handler http.HandlerFunc) (*Runner, string, []byte) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	r := testRunner(t, server)
	material := []byte("fixed bundle bytes")
	path := filepath.Join(t.TempDir(), "result.bundle")
	if err := os.WriteFile(path, material, 0600); err != nil {
		t.Fatal(err)
	}
	return r, path, material
}

func TestArtifactUploadRetriesOnlyTransientResponses(t *testing.T) {
	for _, status := range []int{502, 503, 504, 400, 401, 403, 409, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			r, path, material := artifactUploadTest(t, func(w http.ResponseWriter, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil || string(body) != "fixed bundle bytes" || req.Header.Get("Idempotency-Key") != "fixed-key" {
					t.Error("attempt changed body/key", err)
				}
				if attempts.Add(1) == 1 {
					w.WriteHeader(status)
					return
				}
				respond(w, artifactTestReceipt(body))
			})
			artifact, err := uploadArtifact(context.Background(), r, "run_test", "fixed-key", path, time.Now().Add(time.Minute))
			if status == 502 || status == 503 || status == 504 {
				if err != nil || attempts.Load() != 2 || artifact != artifactTestReceipt(material) {
					t.Fatal("transient delivery did not recover identical material", attempts.Load(), artifact, err)
				}
			} else if err == nil || attempts.Load() != 1 {
				t.Fatal("terminal response was retried", attempts.Load(), err)
			}
		})
	}
}

func TestArtifactUploadPersistentFailureHasFiniteAttempts(t *testing.T) {
	var attempts atomic.Int32
	r, path, _ := artifactUploadTest(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(503)
	})
	start := time.Now()
	_, err := uploadArtifact(context.Background(), r, "run_test", "fixed", path, start.Add(time.Minute))
	if err == nil || attempts.Load() != 3 || time.Since(start) >= 5*time.Second {
		t.Fatal("unbounded upload retries", attempts.Load(), time.Since(start), err)
	}
}

func TestArtifactUploadRejectsMismatchedReceiptWithoutRetry(t *testing.T) {
	for _, field := range []string{"id", "run", "sha256", "size"} {
		t.Run(field, func(t *testing.T) {
			var attempts atomic.Int32
			r, path, _ := artifactUploadTest(t, func(w http.ResponseWriter, req *http.Request) {
				attempts.Add(1)
				body, _ := io.ReadAll(req.Body)
				artifact := artifactTestReceipt(body)
				switch field {
				case "id":
					artifact.ID = "other"
				case "run":
					artifact.RunID = "other"
				case "sha256":
					artifact.SHA256 = "other"
				case "size":
					artifact.Size++
				}
				respond(w, artifact)
			})
			if _, err := uploadArtifact(context.Background(), r, "run_test", "fixed", path, time.Now().Add(time.Minute)); err == nil || attempts.Load() != 1 {
				t.Fatal("mismatched complete receipt accepted/retried", err, attempts.Load())
			}
		})
	}
}

func TestArtifactUploadCancellationAndExpiryStopBackoff(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(map[bool]string{false: "expiry", true: "cancel"}[cancelEarly], func(t *testing.T) {
			var attempts atomic.Int32
			first := make(chan struct{})
			r, path, _ := artifactUploadTest(t, func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					close(first)
				}
				w.WriteHeader(503)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			until := time.Now().Add(100 * time.Millisecond)
			if cancelEarly {
				until = time.Now().Add(time.Minute)
			}
			done := make(chan error, 1)
			go func() { _, err := uploadArtifact(ctx, r, "run_test", "fixed", path, until); done <- err }()
			select {
			case <-first:
			case err := <-done:
				t.Fatal("upload failed before the first request", err)
			case <-time.After(time.Second):
				t.Fatal("upload did not start")
			}
			if cancelEarly {
				time.Sleep(40 * time.Millisecond)
				cancel()
			}
			select {
			case err := <-done:
				if err == nil || attempts.Load() != 1 {
					t.Fatal("backoff ignored expired/revoked permission", err, attempts.Load())
				}
			case <-time.After(time.Second):
				t.Fatal("backoff did not stop promptly")
			}
		})
	}
}

func TestArtifactUploadFreezesBytesInsteadOfReopeningChangedPath(t *testing.T) {
	var attempts atomic.Int32
	var path string
	r, candidate, material := artifactUploadTest(t, func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		if !bytes.Equal(body, []byte("fixed bundle bytes")) {
			t.Error("retry reopened changed path")
		}
		if attempts.Add(1) == 1 {
			if err := os.WriteFile(path, []byte("replacement material"), 0600); err != nil {
				t.Error(err)
			}
			w.WriteHeader(503)
			return
		}
		respond(w, artifactTestReceipt(body))
	})
	path = candidate
	artifact, err := uploadArtifact(context.Background(), r, "run_test", "fixed", path, time.Now().Add(time.Minute))
	if err != nil || artifact != artifactTestReceipt(material) || attempts.Load() != 2 {
		t.Fatal("fixed in-memory material not retained", artifact, err)
	}
}

func TestArtifactUploadRejectsInvalidMaterialBeforeNetwork(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "symlink", "empty", "oversized", "expired"} {
		t.Run(kind, func(t *testing.T) {
			var attempts atomic.Int32
			r, path, _ := artifactUploadTest(t, func(http.ResponseWriter, *http.Request) { attempts.Add(1) })
			until := time.Now().Add(time.Minute)
			if kind == "expired" {
				until = time.Now().Add(-time.Second)
			} else {
				prepareInvalidArtifact(t, path, kind)
			}
			if _, err := uploadArtifact(context.Background(), r, "run_test", "fixed", path, until); err == nil || attempts.Load() != 0 {
				t.Fatal("invalid material/permission reached network", err, attempts.Load())
			}
		})
	}
}

func prepareInvalidArtifact(t *testing.T, path, kind string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var err error
	switch kind {
	case "directory":
		err = os.Mkdir(path, 0700)
	case "symlink":
		target := path + ".target"
		if err := os.WriteFile(target, []byte("bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skip("host cannot create a symlink fixture", err)
		}
	case "empty", "oversized":
		var file *os.File
		file, err = os.Create(path)
		if err == nil {
			if kind == "oversized" {
				err = file.Truncate(maxArtifactBytes + 1)
			}
			file.Close()
		}
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestArtifactUploadDoesNotRetryArbitraryErrors(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("invalid complete JSON receipt")} {
		if retryArtifactUpload(err) {
			t.Fatal("nontransient error retried", err)
		}
	}
}

func TestArtifactUploadOwnsRetryCountAndOverallDeadline(t *testing.T) {
	r, path, _ := artifactUploadTest(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected network") })
	attempts := 0
	r.http.Transport = intentTransport(func(req *http.Request) (*http.Response, error) {
		attempts++
		if req.GetBody != nil {
			t.Error("HTTP transport can silently replay the upload")
		}
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Error("upload escaped its fixed five-second window")
		}
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
	})
	if _, err := uploadArtifact(context.Background(), r, "run_test", "fixed", path, time.Now().Add(time.Minute)); err == nil || attempts != 3 {
		t.Fatal("incorrect explicit attempt budget", attempts, err)
	}
}
