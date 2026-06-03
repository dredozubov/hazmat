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
	file string
	line int
}

func TestVerifiedLedgerGovernedFunctionsExist(t *testing.T) {
	refs := loadVerifiedLedgerFunctionRefs(t)
	if len(refs) == 0 {
		t.Fatal("VERIFIED.md governed-code function reference scan found no references")
	}

	functions := loadModuleFunctionIndex(t)
	allowedExternal := map[string]string{
		"sandbox_init": "Apple Seatbelt C API called through cgo in hazmat-launch",
	}

	var missing []string
	for _, ref := range refs {
		if _, ok := allowedExternal[ref.name]; ok {
			continue
		}
		if verifiedFunctionRefExists(ref, functions) {
			continue
		}
		missing = append(missing, describeMissingVerifiedFunction(ref, functions))
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

	fileRe := regexp.MustCompile(`hazmat/[A-Za-z0-9_./*-]+\.go`)
	fnRe := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\(\)`)
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
		files := fileRe.FindAllStringIndex(line, -1)
		for _, match := range fnRe.FindAllStringSubmatchIndex(line, -1) {
			file := nearestVerifiedLedgerFile(line, files, match[0])
			refs = append(refs, verifiedFunctionRef{
				name: line[match[2]:match[3]],
				file: file,
				line: lineNo,
			})
		}
	}
	return refs
}

func nearestVerifiedLedgerFile(line string, files [][]int, fnStart int) string {
	file := ""
	for _, loc := range files {
		if loc[1] > fnStart {
			break
		}
		file = line[loc[0]:loc[1]]
	}
	return file
}

type verifiedFunctionIndex struct {
	byName map[string]map[string]bool
	byFile map[string]map[string]bool
}

func loadModuleFunctionIndex(t *testing.T) verifiedFunctionIndex {
	t.Helper()

	functions := verifiedFunctionIndex{
		byName: make(map[string]map[string]bool),
		byFile: make(map[string]map[string]bool),
	}
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
		ledgerPath := filepath.ToSlash(filepath.Join("hazmat", path))
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			functions.add(ledgerPath, fn.Name.Name)
			if fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if recv := receiverName(fn.Recv.List[0].Type); recv != "" {
				functions.add(ledgerPath, recv+"."+fn.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go sources: %v", err)
	}
	return functions
}

func (idx verifiedFunctionIndex) add(file, name string) {
	if idx.byName[name] == nil {
		idx.byName[name] = make(map[string]bool)
	}
	idx.byName[name][file] = true
	if idx.byFile[file] == nil {
		idx.byFile[file] = make(map[string]bool)
	}
	idx.byFile[file][name] = true
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

func verifiedFunctionRefExists(ref verifiedFunctionRef, functions verifiedFunctionIndex) bool {
	if ref.file != "" && !strings.Contains(ref.file, "*") {
		return functions.byFile[ref.file][ref.name]
	}
	return verifiedFunctionExistsAnywhere(ref.name, functions)
}

func verifiedFunctionExistsAnywhere(name string, functions verifiedFunctionIndex) bool {
	if len(functions.byName[name]) > 0 {
		return true
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return len(functions.byName[name[idx+1:]]) > 0
	}
	return false
}

func describeMissingVerifiedFunction(ref verifiedFunctionRef, functions verifiedFunctionIndex) string {
	location := ref.name + " at tla/VERIFIED.md:" + strconv.Itoa(ref.line)
	if ref.file == "" || strings.Contains(ref.file, "*") {
		return location
	}
	if files := functions.byName[ref.name]; len(files) > 0 {
		return location + " cites " + ref.file + " but declaration is in " + strings.Join(sortedKeys(files), ", ")
	}
	return location + " cites " + ref.file
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
