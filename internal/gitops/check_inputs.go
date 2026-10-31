package gitops

import (
	"context"
	"strings"
)

// CheckInputPaths reports presence only for explicitly requested policy paths
// in a fixed tree, including generated manifests omitted from public analysis.
// It never reads their contents, working files, symlinks or submodule targets.
func (g *Client) CheckInputPaths(ctx context.Context, repo, sha string, inputs []string) ([]string, error) {
	if err := g.guardRepo(ctx, repo); err != nil {
		return nil, err
	}
	if err := validIDs(OID(sha)); err != nil {
		return nil, err
	}
	for _, input := range inputs {
		if err := ValidatePath(input); err != nil {
			return nil, err
		}
	}
	out, err := g.run(ctx, repo, nil, "ls-tree", "-r", "-z", sha)
	if err != nil {
		return nil, err
	}
	entries := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(entries) > 10000 {
		return nil, ErrOutputLimit
	}
	found := map[string]bool{}
	for _, entry := range entries {
		header, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" {
			continue
		}
		for _, input := range inputs {
			if name == input || strings.HasPrefix(name, input+"/") {
				found[input] = true
			}
		}
	}
	var present []string
	for _, input := range inputs {
		if found[input] {
			present = append(present, input)
		}
	}
	return present, nil
}
