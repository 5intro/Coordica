package gitops

import (
	"errors"
	"path"
	"reflect"
	"testing"
)

func TestValidatePath(t *testing.T) {
	valid := []string{
		"README.md", ".env", "src/a.go", "文件/你好.txt",
		"space name/tab\tand\nnewline.txt", "dir/a:b", "[literal]",
	}
	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			if err := ValidatePath(name); err != nil {
				t.Fatalf("ValidatePath(%q) = %v", name, err)
			}
		})
	}
	invalid := []string{
		"", "/src/a.go", "//server/share", "C:/src/a.go", "C:relative",
		"c:relative", "src\\a.go", "src/\x00a.go", ".", "..",
		"./a.go", "src/../a.go", "src/./a.go", "src//a.go", "src/",
		string([]byte{'a', '/', 0xff}),
	}
	for _, name := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			if err := ValidatePath(name); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("ValidatePath(%q) = %v, want ErrInvalidPath", name, err)
			}
		})
	}
}

func TestScopeMatching(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		allowed []string
		denied  []string
	}{
		{
			name: "exact", pattern: "src/main.go",
			allowed: []string{"src/main.go"},
			denied:  []string{"src/Main.go", "src/main.go.bak", "main.go", "src/nested/main.go"},
		},
		{
			name: "directory descendants", pattern: "src/",
			allowed: []string{"src/main.go", "src/nested/main.go", "src/.hidden"},
			denied:  []string{"src", "src-other/main.go", "x/src/main.go"},
		},
		{
			name: "segment glob", pattern: "src/*.[ch]",
			allowed: []string{"src/main.c", "src/x.h"},
			denied:  []string{"src/main.go", "src/nested/main.c", "main.c"},
		},
		{
			name: "Unicode question mark", pattern: "文件/?.txt",
			allowed: []string{"文件/你.txt", "文件/a.txt"},
			denied:  []string{"文件/你好.txt", "文件/.txt"},
		},
		{
			name: "recursive root", pattern: "**/*.go",
			allowed: []string{"main.go", "src/main.go", "src/nested/main.go"},
			denied:  []string{"main.txt", "src/main.go.bak"},
		},
		{
			name: "recursive middle zero segments", pattern: "src/**/main.go",
			allowed: []string{"src/main.go", "src/a/main.go", "src/a/b/main.go"},
			denied:  []string{"main.go", "src/main.go/other", "src/a/main.txt"},
		},
		{
			name: "recursive suffix zero segments", pattern: "src/**",
			allowed: []string{"src", "src/main.go", "src/a/b/main.go"},
			denied:  []string{"src-other", "other/src"},
		},
		{
			name: "multiple recursive segments", pattern: "**/**/main.go",
			allowed: []string{"main.go", "a/main.go", "a/b/main.go"},
			denied:  []string{"main.txt", "main.go/a"},
		},
		{
			name: "recursive glob directory", pattern: "packages/*/",
			allowed: []string{"packages/api/main.go", "packages/web/src/index.ts"},
			denied:  []string{"packages/api", "packages/main.go", "other/api/main.go"},
		},
		{
			name: "all paths", pattern: "**",
			allowed: []string{"root", "a/b/c", ".hidden"},
		},
		{
			name: "special characters", pattern: "文件/space \t\n.go",
			allowed: []string{"文件/space \t\n.go"},
			denied:  []string{"文件/space.go"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scope, err := NewScope([]string{test.pattern}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range test.allowed {
				if result := scope.Check([]string{name}); !result.Allowed {
					t.Errorf("pattern %q denied %q: %+v", test.pattern, name, result)
				}
			}
			for _, name := range test.denied {
				result := scope.Check([]string{name})
				if result.Allowed || !reflect.DeepEqual(result.OutOfScope, []string{name}) {
					t.Errorf("pattern %q accepted %q or omitted finding: %+v", test.pattern, name, result)
				}
			}
		})
	}
}

func TestScopeProtectionDoesNotGrantAuthorization(t *testing.T) {
	scope, err := NewScope([]string{"src/", "policy/current.json"}, []string{"policy/", "src/secrets/"})
	if err != nil {
		t.Fatal(err)
	}
	result := scope.Check([]string{
		"src/main.go", "policy/current.json", "policy/new.json", "src/secrets/token",
		"z.txt", "a.txt", "z.txt", "policy/new.json", "../policy/current.json",
	})
	want := ScopeResult{
		Allowed:    false,
		OutOfScope: []string{"../policy/current.json", "a.txt", "policy/new.json", "z.txt"},
		Protected:  []string{"policy/current.json", "policy/new.json", "src/secrets/token"},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("Check() = %+v, want %+v", result, want)
	}
	result = scope.Check([]string{"policy/current.json"})
	if !result.Allowed || !reflect.DeepEqual(result.Protected, []string{"policy/current.json"}) {
		t.Fatalf("allowed protected path lost independent findings: %+v", result)
	}
}

func TestScopeEmptyAllowListAndInput(t *testing.T) {
	scope, err := NewScope(nil, []string{"**"})
	if err != nil {
		t.Fatal(err)
	}
	if result := scope.Check(nil); !result.Allowed || result.OutOfScope != nil || result.Protected != nil {
		t.Fatalf("empty input = %+v", result)
	}
	result := scope.Check([]string{"a.go"})
	if result.Allowed || !reflect.DeepEqual(result.OutOfScope, []string{"a.go"}) || !reflect.DeepEqual(result.Protected, []string{"a.go"}) {
		t.Fatalf("empty allow list = %+v", result)
	}
}

func TestScopeRejectsInvalidPathsEvenWhenAllowAll(t *testing.T) {
	scope, err := NewScope([]string{"**"}, []string{"**"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{"../escape", "/absolute", "src//x", "src\\x", "C:relative", "src/x\x00"}
	result := scope.Check(invalid)
	if result.Allowed || len(result.OutOfScope) != len(invalid) || len(result.Protected) != 0 {
		t.Fatalf("invalid paths = %+v", result)
	}
}

func TestNewScopeRejectsInvalidPatterns(t *testing.T) {
	invalidPaths := []string{"", "/", "/**", "//host/share", "C:/**", "../**", "src/./*", "src//", "src\\*", "src/\x00*", string([]byte{0xff})}
	for _, pattern := range invalidPaths {
		t.Run("path/"+pattern, func(t *testing.T) {
			if _, err := NewScope([]string{pattern}, nil); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("NewScope(%q) = %v, want ErrInvalidPath", pattern, err)
			}
		})
	}
	for _, pattern := range []string{"[", "src/[]", "src/[a-]", "src/[abc"} {
		t.Run("glob/"+pattern, func(t *testing.T) {
			if _, err := NewScope([]string{pattern}, nil); !errors.Is(err, path.ErrBadPattern) {
				t.Fatalf("NewScope(%q) = %v, want ErrBadPattern", pattern, err)
			}
		})
	}
	if _, err := NewScope([]string{"**"}, []string{"../policy"}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("invalid protected pattern = %v, want ErrInvalidPath", err)
	}
}
