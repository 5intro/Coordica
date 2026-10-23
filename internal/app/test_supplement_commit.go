package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
)

func (c TestCatalog) requireSupplementCommit(ctx context.Context, commit gitops.OID) error {
	g, ok := c.Service.Source.(*gitops.Client)
	if !ok {
		g = gitops.New("git")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := g.RequireCommit(ctx, filepath.Join(c.Service.Dir, "repository"), commit)
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, process.ErrTreeNotStopped) || errors.Is(err, gitops.ErrUnsafeConfig) {
		return err
	}
	var command *gitops.Error
	if errors.Is(err, gitops.ErrUnsupportedObject) || errors.As(err, &command) && command.ExitCode > 0 {
		return domain.Fail("invalid_input", "中心仓库没有该固定提交，请先同步项目或选择有效提交")
	}
	return err
}
