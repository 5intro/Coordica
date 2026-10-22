// qualitycheck uses the same fixed policy as the center's merge gate.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"coordica/internal/codequality"
	"coordica/internal/gitops"
)

func main() {
	base := flag.String("base", "", "Required full target-main commit; never a candidate allowlist")
	candidate := flag.String("candidate", "", "Full candidate commit (or use --working-tree)")
	working := flag.Bool("working-tree", false, "Check tracked and nonignored new local source")
	repo := flag.String("repository", ".", "Fixed local repository")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := check(ctx, *repo, *base, *candidate, *working); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(ctx context.Context, repo, base, candidate string, working bool) error {
	b, err := gitops.ParseOID(base)
	if err != nil || working == (candidate != "") {
		return fmt.Errorf("require a full --base and exactly one of --candidate/--working-tree")
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	reader := gitops.SourceReader{Git: gitops.New(git)}
	var old, next []codequality.File
	appendTo := func(files *[]codequality.File) func(string, string) error {
		return func(path, text string) error {
			*files = append(*files, codequality.File{Path: path, Text: text})
			return nil
		}
	}
	if err = reader.Read(ctx, abs, b, codequality.Included, appendTo(&old)); err != nil {
		return err
	}
	if working {
		err = reader.Worktree(ctx, abs, codequality.Included, appendTo(&next))
	} else {
		var c gitops.OID
		if c, err = gitops.ParseOID(candidate); err == nil {
			err = reader.Read(ctx, abs, c, codequality.Included, appendTo(&next))
		}
	}
	if err != nil {
		return err
	}
	return compare(ctx, old, next)
}

func compare(ctx context.Context, old, next []codequality.File) error {
	findings, err := codequality.RegressionsContext(ctx, old, next)
	if err != nil {
		return err
	}
	for _, finding := range findings[:min(30, len(findings))] {
		fmt.Fprintln(os.Stderr, finding)
	}
	if len(findings) != 0 {
		return fmt.Errorf("%s: %d regressions; merge refused", codequality.Version, len(findings))
	}
	fmt.Printf("%s: no new or increased structural debt\n", codequality.Version)
	return nil
}
