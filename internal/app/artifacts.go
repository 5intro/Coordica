package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func (s *Service) UploadArtifact(ctx context.Context, a Actor, id string, body io.Reader) (domain.Artifact, error) {
	var result domain.Artifact
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		_, err := artifactUploadRun(tx, a, id, s.Now())
		return err
	})
	if e != nil {
		return result, e
	}
	dir := filepath.Join(s.Dir, "artifacts")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return result, e
	}
	file, e := os.CreateTemp(dir, "upload-")
	if e != nil {
		return result, e
	}
	temp := file.Name()
	defer os.Remove(temp)
	defer file.Close()
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(file, h), io.LimitReader(body, (64<<20)+1))
	if e != nil {
		return result, e
	}
	if n > 64<<20 {
		return result, domain.Fail("invalid_input", "代码材料超过64MiB")
	}
	if e = file.Sync(); e != nil {
		return result, e
	}
	if e = file.Close(); e != nil {
		return result, e
	}
	digest := hex.EncodeToString(h.Sum(nil))
	result = domain.Artifact{ID: id + "_" + digest, RunID: id, SHA256: digest, Size: n, CreatedAt: s.Now()}
	if e = os.Rename(temp, filepath.Join(dir, result.ID)); e != nil {
		return result, e
	}
	e = s.DB.Update(ctx, func(tx *store.Tx) error {
		if _, e := artifactUploadRun(tx, a, id, s.Now()); e != nil {
			return e
		}
		var prior domain.Artifact
		if e := tx.Get("artifact", result.ID, &prior); e == nil {
			result = prior
			return nil
		} else if !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		if e := tx.Put("artifact", result.ID, result); e != nil {
			return e
		}
		return s.audit(tx, a, "artifact.accepted", result.ID, result.SHA256)
	})
	return result, e
}

func (s *Service) InputRun(ctx context.Context, a Actor, id string) (domain.Run, error) {
	r, e := s.AssignmentRun(ctx, a, id)
	if e == nil && (!s.Now().Before(r.LeaseUntil) || r.State != "running") {
		e = domain.Fail("stale_run", "执行租约已失效")
	}
	if e == nil {
		e = s.DB.View(ctx, func(tx *store.Tx) error { return baselineRun(tx, r) })
	}
	return r, e
}
