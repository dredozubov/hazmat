package hazmat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestReadOnlyDiagnosticsDoNotUsePromptingSudo(t *testing.T) {
	files := []string{
		"test.go",
		"setup_verification_darwin.go",
	}
	blocked := map[string]struct{}{
		"sudo":           {},
		"newSudoCommand": {},
	}

	for _, name := range files {
		path := filepath.Join(".", name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if _, found := blocked[ident.Name]; !found {
				return true
			}
			pos := fset.Position(ident.Pos())
			t.Fatalf("%s uses prompt-capable %s; use sudoNoPrompt, sudoOutput, or helper-backed noninteractive probes", pos, ident.Name)
			return false
		})
	}
}
