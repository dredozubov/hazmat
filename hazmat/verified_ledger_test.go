package hazmat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type verifiedFunctionRef struct {
	name string
	line int
}

func TestVerifiedLedgerGovernedFunctionsExist(t *testing.T) {
	refs := loadVerifiedLedgerFunctionRefs(t)
	if len(refs) == 0 {
		t.Fatal("VERIFIED.md governed-code function reference scan found no references")
	}

	functions := loadModuleFunctionNames(t)
	allowedExternal := map[string]string{
		"sandbox_init": "Apple Seatbelt C API called through cgo in hazmat-launch",
	}

	var missing []string
	for _, ref := range refs {
		if _, ok := allowedExternal[ref.name]; ok {
			continue
		}
		if verifiedFunctionExists(ref.name, functions) {
			continue
		}
		missing = append(missing, ref.name+" at tla/VERIFIED.md:"+strconv.Itoa(ref.line))
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("VERIFIED.md cites governed functions that do not exist in Go source:\n%s", strings.Join(missing, "\n"))
	}
}

func loadVerifiedLedgerFunctionRefs(t *testing.T) []verifiedFunctionRef {
	t.Helper()

	path := filepath.Join("..", "tla", "VERIFIED.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	re := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\(\)`)
	var refs []verifiedFunctionRef
	inQuickReference := false
	for idx, line := range strings.Split(string(raw), "\n") {
		lineNo := idx + 1
		if strings.HasPrefix(line, "## Quick Reference: Spec") {
			inQuickReference = true
			continue
		}
		if inQuickReference && strings.HasPrefix(line, "## ") {
			inQuickReference = false
		}
		relevant := strings.HasPrefix(line, "| Governed code |") || (inQuickReference && strings.HasPrefix(line, "| `"))
		if !relevant || !strings.Contains(line, ".go") {
			continue
		}
		for _, match := range re.FindAllStringSubmatch(line, -1) {
			refs = append(refs, verifiedFunctionRef{
				name: match[1],
				line: lineNo,
			})
		}
	}
	return refs
}

func loadModuleFunctionNames(t *testing.T) map[string]bool {
	t.Helper()

	functions := make(map[string]bool)
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			functions[fn.Name.Name] = true
			if fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if recv := receiverName(fn.Recv.List[0].Type); recv != "" {
				functions[recv+"."+fn.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go sources: %v", err)
	}
	return functions
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}

func verifiedFunctionExists(name string, functions map[string]bool) bool {
	if functions[name] {
		return true
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return functions[name[idx+1:]]
	}
	return false
}
