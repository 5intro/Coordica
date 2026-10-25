package codequality

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
)

func receiverName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return receiverName(v.X)
	case *ast.IndexExpr:
		return receiverName(v.X)
	case *ast.IndexListExpr:
		return receiverName(v.X)
	case *ast.ParenExpr:
		return receiverName(v.X)
	}
	return ""
}

func (m *metrics) goSource(f File) error {
	set := token.NewFileSet()
	root, err := parser.ParseFile(set, f.Path, f.Text, parser.ParseComments)
	if err != nil {
		return &SourceError{Cause: fmt.Errorf("Go structural analysis %s: %w", f.Path, err)}
	}
	for _, decl := range root.Decls {
		if m.cancelled() {
			return m.ctx.Err()
		}
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			receiver := receiverName(fn.Recv.List[0].Type)
			name = receiver + "." + name
			key := path.Dir(f.Path) + "/" + root.Name.Name + "/" + receiver
			m.methods[key] = append(m.methods[key], Finding{Path: f.Path, Line: m.lineAt(set.Position(fn.Pos()).Offset)})
		}
		if fn.Body != nil {
			start, end := m.lineAt(set.Position(fn.Pos()).Offset), m.lineAt(set.Position(fn.End()).Offset)
			m.add(f.Path, start, "function_lines", name, end-start+1, MaxFunction)
		}
	}
	m.goLiterals(f, set, root)
	return nil
}
