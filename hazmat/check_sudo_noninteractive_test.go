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
	blockedCalls := map[string]string{
		"exec.Command":            "direct command construction can bypass noninteractive sudo policy",
		"hostexec.NewSudoCommand": "use sudo -n or helper-backed probes in read-only diagnostics",
		"hostexec.RunInteractive": "read-only diagnostics must not prompt",
		"hostexec.Sudo":           "use sudoNoPrompt, sudoOutput, or helper-backed noninteractive probes",
		"hostexec.SudoAppendFile": "read-only diagnostics must not mutate host files",
		"hostexec.SudoWriteFile":  "read-only diagnostics must not mutate host files",
		"newSudoCommand":          "use sudo -n or helper-backed probes in read-only diagnostics",
		"runInteractive":          "read-only diagnostics must not prompt",
		"sudo":                    "use sudoNoPrompt, sudoOutput, or helper-backed noninteractive probes",
		"sudoAppendFile":          "read-only diagnostics must not mutate host files",
		"sudoWriteFile":           "read-only diagnostics must not mutate host files",
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
			name, ok := diagnosticCallName(call.Fun)
			if !ok {
				return true
			}
			reason, found := blockedCalls[name]
			if !found {
				return true
			}
			pos := fset.Position(call.Fun.Pos())
			t.Fatalf("%s uses %s; %s", pos, name, reason)
			return false
		})
	}
}

func diagnosticCallName(expr ast.Expr) (string, bool) {
	switch fun := expr.(type) {
	case *ast.Ident:
		return fun.Name, true
	case *ast.SelectorExpr:
		pkg, ok := fun.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		return pkg.Name + "." + fun.Sel.Name, true
	default:
		return "", false
	}
}
