package codequality

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
)

func goNodeHeader(set *token.FileSet, node ast.Node) string {
	var out bytes.Buffer
	if node != nil {
		_ = format.Node(&out, set, node)
	}
	return out.String()
}

func goLiteralScope(set *token.FileSet, node ast.Node) string {
	switch n := node.(type) {
	case *ast.FuncDecl:
		receiver := ""
		if n.Recv != nil {
			receiver = goNodeHeader(set, n.Recv)
		}
		return receiver + n.Name.Name + goNodeHeader(set, n.Type)
	case *ast.FuncLit:
		return goNodeHeader(set, n.Type)
	case *ast.ValueSpec:
		var names []string
		for _, name := range n.Names {
			names = append(names, name.Name)
		}
		return "binding " + strings.Join(names, ",")
	case *ast.AssignStmt:
		var names []string
		for _, lhs := range n.Lhs {
			names = append(names, goNodeHeader(set, lhs))
		}
		return "assignment " + strings.Join(names, ",")
	case *ast.CallExpr:
		var call strings.Builder
		if fn, ok := n.Fun.(*ast.FuncLit); ok {
			call.WriteString(goNodeHeader(set, fn.Type))
		} else {
			call.WriteString(goNodeHeader(set, n.Fun))
		}
		for i, arg := range n.Args {
			if lit, ok := arg.(*ast.BasicLit); ok {
				fmt.Fprintf(&call, " argument%d=%s", i, lit.Value)
			}
		}
		return "call " + call.String()
	case *ast.KeyValueExpr:
		return "key " + goNodeHeader(set, n.Key)
	}
	return ""
}

func (m *metrics) goLiterals(f File, set *token.FileSet, root *ast.File) {
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if m.cancelled() {
			return false
		}
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		stack = append(stack, node)
		fn, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		start, end := set.Position(fn.Pos()).Line, set.Position(fn.End()).Line
		if end-start+1 <= MaxFunction {
			return true
		}
		var context strings.Builder
		for _, ancestor := range stack {
			if header := goLiteralScope(set, ancestor); header != "" {
				context.WriteString(header)
				context.WriteByte(0)
			}
		}
		m.add(f.Path, start, "function_lines", "literal", end-start+1, MaxFunction)
		m.findings[len(m.findings)-1].key += fmt.Sprintf("\x00%x", sha256.Sum256([]byte(context.String())))
		return true
	})
}
