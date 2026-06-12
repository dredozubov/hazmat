package hazmat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyDiagnosticsDoNotUsePromptingSudo(t *testing.T) {
	files := []string{
		"test.go",
		"setup_verification_darwin.go",
	}
	blockedCalls := map[string]string{
		"exec.Command":                      "direct command construction can bypass read-only diagnostic policy",
		"genericAgentPasswordlessAvailable": "read-only diagnostics must not execute generic sudo probes",
		"hostexec.NewSudoCommand":           "read-only diagnostics must not execute sudo probes",
		"hostexec.RunInteractive":           "read-only diagnostics must not prompt",
		"hostexec.Sudo":                     "read-only diagnostics must not execute sudo probes",
		"hostexec.SudoAppendFile":           "read-only diagnostics must not mutate host files",
		"hostexec.SudoWriteFile":            "read-only diagnostics must not mutate host files",
		"newSudoCommand":                    "read-only diagnostics must not execute sudo probes",
		"newSudoNoPromptCommand":            "read-only diagnostics must not execute sudo probes",
		"runInteractive":                    "read-only diagnostics must not prompt",
		"sudo":                              "read-only diagnostics must not execute sudo probes",
		"sudoAppendFile":                    "read-only diagnostics must not mutate host files",
		"sudoNoPrompt":                      "read-only diagnostics must not execute sudo probes",
		"sudoOutput":                        "read-only diagnostics must not execute sudo read probes",
		"sudoWriteFile":                     "read-only diagnostics must not mutate host files",
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

func TestReadOnlyDiagnosticsAvoidPromptLikeAdvice(t *testing.T) {
	files := []string{
		"test.go",
		"setup_verification_darwin.go",
	}
	blockedText := []string{
		"still prompts",
		"will still prompt",
		"run: sudo",
		"try: sudo",
		"password is required",
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		content := string(data)
		for _, blocked := range blockedText {
			if strings.Contains(content, blocked) {
				t.Fatalf("%s contains prompt-like read-only diagnostic copy %q", name, blocked)
			}
		}
	}
}

func TestPreGateDiagnosticsDoNotUseAgentHelperProbes(t *testing.T) {
	preGateFunctions := map[string]bool{
		"inspectAgentProbeGate":    true,
		"testAgentUser":            true,
		"testDevGroupAndWorkspace": true,
		"testPasswordlessSudo":     true,
		"testPfFirewallStatic":     true,
		"testDNSBlocklist":         true,
		"testPersistence":          true,
	}
	blockedCalls := map[string]string{
		"agentPathExists":       "pre-gate diagnostics must not switch to the agent helper",
		"agentPathIsDir":        "pre-gate diagnostics must not switch to the agent helper",
		"agentPathIsExecutable": "pre-gate diagnostics must not switch to the agent helper",
		"agentPathIsSymlink":    "pre-gate diagnostics must not switch to the agent helper",
		"agentReadDirNames":     "pre-gate diagnostics must not switch to the agent helper",
		"agentReadFile":         "pre-gate diagnostics must not switch to the agent helper",
		"asAgentCombinedOutput": "pre-gate diagnostics must not switch to the agent helper",
		"asAgentOutput":         "pre-gate diagnostics must not switch to the agent helper",
		"asAgentQuiet":          "pre-gate diagnostics must not switch to the agent helper",
	}

	path := filepath.Join(".", "test.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !preGateFunctions[fn.Name.Name] {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
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
			t.Fatalf("%s uses %s in %s; %s", pos, name, fn.Name.Name, reason)
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
