package codequality

import (
	"context"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

type csharpProject struct {
	name, root     string
	defaultCompile bool
	items          []csharpCompileItem
	err            error
}

func csharpMetadata(name string) bool {
	return strings.EqualFold(path.Ext(name), ".csproj") ||
		path.Base(name) == "Directory.Build.props" || path.Base(name) == "Directory.Build.targets"
}

func collectCSharpProjects(ctx context.Context, files []File) ([]csharpProject, error) {
	var projects []csharpProject
	var imports []string
	total, count := 0, 0
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !Included(f.Path) {
			continue
		}
		total += len(f.Text)
		count++
		if len(f.Text) > MaxFileBytes || total > MaxTreeBytes || count > MaxTreeFiles {
			return nil, fmt.Errorf("source inventory size limit at %s", f.Path)
		}
		if !csharpMetadata(f.Path) {
			continue
		}
		if !strings.EqualFold(path.Ext(f.Path), ".csproj") {
			imports = append(imports, f.Path)
			continue
		}
		p := csharpProject{name: f.Path, root: path.Dir(f.Path)}
		if !utf8.ValidString(f.Text) || strings.ContainsRune(f.Text, 0) {
			p.err = fmt.Errorf("project is not UTF-8 text")
		} else {
			p.err = parseCSharpProject(f.Text, &p)
		}
		projects = append(projects, p)
		if len(projects) > 256 {
			return nil, fmt.Errorf("C# project inventory exceeds 256 scopes")
		}
	}
	for i := range projects {
		for _, name := range imports {
			if _, ok := csharpRelative(path.Dir(name), projects[i].name); ok {
				projects[i].err = fmt.Errorf("external build imports require native project analysis: %s", name)
			}
		}
	}
	return projects, nil
}

func csharpSourceScopes(projects []csharpProject, name string) ([]string, error) {
	if len(projects) == 0 {
		return []string{""}, nil
	}
	var scopes []string
	for _, p := range projects {
		relative, inside := csharpRelative(p.root, name)
		if !inside {
			relative = name
		}
		if p.err != nil {
			return nil, &SourceError{Cause: fmt.Errorf("C# compilation scope %s: %w", p.name, p.err)}
		}
		included := inside && p.defaultCompile && !csharpBuildArtifact(relative)
		for _, item := range p.items {
			if csharpMatches(item.includes, name) && !csharpMatches(item.excludes, name) {
				included = true
			}
			if csharpMatches(item.removes, name) {
				included = false
			}
		}
		if included {
			scopes = append(scopes, p.name)
			if len(scopes) > 256 {
				return nil, &SourceError{Cause: fmt.Errorf("too many C# compilation scopes for %s", name)}
			}
		}
	}
	// Sources outside statically resolved projects cannot donate debt to another file.
	if len(scopes) == 0 {
		scopes = []string{"unassigned:" + name}
	}
	return scopes, nil
}

func csharpRelative(root, name string) (string, bool) {
	if root == "." {
		return name, !strings.HasPrefix(name, "../")
	}
	relative, ok := strings.CutPrefix(name, root+"/")
	return relative, ok
}

func csharpBuildArtifact(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "bin" || part == "obj" || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}
