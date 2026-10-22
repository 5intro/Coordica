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
	}
	return ""
}

func (m *metrics) goSource(f File) error {
	set := token.NewFileSet()
	root, err := parser.ParseFile(set, f.Path, f.Text, 0)
	if err != nil {
		return fmt.Errorf("Go structural analysis %s: %w", f.Path, err)
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
			m.methods[key] = append(m.methods[key], Finding{Path: f.Path, Line: set.Position(fn.Pos()).Line})
		}
		if fn.Body != nil {
			start, end := set.Position(fn.Pos()).Line, set.Position(fn.End()).Line
			m.add(f.Path, start, "function_lines", name, end-start+1, MaxFunction)
		}
	}
	m.goLiterals(f, set, root)
	return nil
}
