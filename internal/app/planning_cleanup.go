package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Stop evidence is committed before filesystem I/O. Keep the recorded path on
// failure so reconciliation (or an idempotent confirmation replay) can retry.
func (s *Service) cleanupPlanningDirectory(ctx context.Context, id string) error {
	var r domain.PlanningRun
	if err := s.DB.View(ctx, func(tx *store.Tx) error { return tx.Get("planning_run", id, &r) }); err != nil {
		return err
	}
	if !r.TreeStopped || r.WorkingDirectory == "" {
		return nil
	}
	dir := filepath.Clean(r.WorkingDirectory)
	prefix := "coordica-planning-" + r.ID + "-" + fmt.Sprint(r.Generation) + "-"
	root, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(dir))
	if err != nil {
		return err
	}
	if !filepath.IsAbs(r.WorkingDirectory) || parent != root || !strings.HasPrefix(filepath.Base(dir), prefix) || filepath.Base(dir) == prefix {
		return errors.New("refusing cleanup outside the recorded planning temporary directory")
	}
	if info, err := os.Lstat(dir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("planning temporary directory is not an ordinary directory")
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.PlanningRun
		if err := tx.Get("planning_run", id, &current); err != nil {
			return err
		}
		if !current.TreeStopped || current.Generation != r.Generation || current.WorkingDirectory != r.WorkingDirectory {
			return nil
		}
		current.WorkingDirectory = ""
		return tx.Put("planning_run", id, current)
	})
}
