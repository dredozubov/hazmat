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

func TestFormalVerificationDocsMentionPromotedSpecs(t *testing.T) {
	promoted := loadPromotedSpecs(t)
	if len(promoted) == 0 {
		t.Fatal("promoted spec roster is empty")
	}

	docs := []string{
		filepath.Join("..", "CLAUDE.md"),
		filepath.Join("..", "tla", "README.md"),
		filepath.Join("..", "tla", "VERIFIED.md"),
	}
	for _, doc := range docs {
		mentioned := specsMentionedInMarkdown(t, doc)
		assertSpecSetEqual(t, doc, mentioned, promoted)
	}

	checkSuite := filepath.Join("..", "tla", "check_suite.sh")
	assertSpecSetEqual(t, checkSuite, specsMentionedInFile(t, checkSuite), promoted)
}

func loadPromotedSpecs(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "tla", "promoted_specs.tsv")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	specs := make(map[string]bool)
	for idx, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx == 0 {
			if line != "spec\tliveness" {
				t.Fatalf("%s: unexpected header %q", path, line)
			}
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			t.Fatalf("%s:%d: expected two tab-separated columns, got %q", path, idx+1, line)
		}
		specs[fields[0]] = true
	}
	return specs
}

func specsMentionedInMarkdown(t *testing.T, path string) map[string]bool {
	t.Helper()
	mentioned := specsMentionedInFile(t, path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	for spec := range mentioned {
		if strings.Contains(text, spec+".tla") || strings.Contains(text, spec+".*") {
			continue
		}
		if strings.Contains(text, "`"+spec+"`") {
			continue
		}
		t.Fatalf("%s mentions %s without code formatting, .tla, or .* context", path, spec)
	}
	return mentioned
}

func specsMentionedInFile(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	re := regexp.MustCompile(`\bMC_[A-Za-z0-9_]+\b`)
	mentioned := make(map[string]bool)
	for _, spec := range re.FindAllString(string(raw), -1) {
		mentioned[spec] = true
	}
	return mentioned
}

func assertSpecSetEqual(t *testing.T, label string, got, want map[string]bool) {
	t.Helper()
	var missing []string
	for spec := range want {
		if !got[spec] {
			missing = append(missing, spec)
		}
	}
	var extra []string
	for spec := range got {
		if !want[spec] {
			extra = append(extra, spec)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return
	}
	sort.Strings(missing)
	sort.Strings(extra)
	t.Fatalf("%s promoted spec references drifted\nmissing: %v\nextra: %v", label, missing, extra)
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

type verifiedCallableAlias struct {
	file   string
	name   string
	target string
}

func loadModuleFunctionIndex(t *testing.T) verifiedFunctionIndex {
	t.Helper()

	functions := verifiedFunctionIndex{
		byName: make(map[string]map[string]bool),
		byFile: make(map[string]map[string]bool),
	}
	var aliases []verifiedCallableAlias
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
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				functions.add(ledgerPath, decl.Name.Name)
				if decl.Recv == nil || len(decl.Recv.List) == 0 {
					continue
				}
				if recv := receiverName(decl.Recv.List[0].Type); recv != "" {
					functions.add(ledgerPath, recv+"."+decl.Name.Name)
				}
			case *ast.GenDecl:
				aliases = append(aliases, functionAliases(ledgerPath, decl)...)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go sources: %v", err)
	}
	for _, alias := range aliases {
		if alias.target == "" || verifiedFunctionExistsAnywhere(alias.target, functions) {
			functions.add(alias.file, alias.name)
		}
	}
	return functions
}

func functionAliases(file string, decl *ast.GenDecl) []verifiedCallableAlias {
	if decl.Tok != token.VAR {
		return nil
	}
	var aliases []verifiedCallableAlias
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for idx, name := range valueSpec.Names {
			value := valueForName(valueSpec.Values, idx)
			target, ok := callableAliasTarget(value)
			if !ok {
				continue
			}
			aliases = append(aliases, verifiedCallableAlias{
				file:   file,
				name:   name.Name,
				target: target,
			})
		}
	}
	return aliases
}

func valueForName(values []ast.Expr, idx int) ast.Expr {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 || idx >= len(values) {
		return values[0]
	}
	return values[idx]
}

func callableAliasTarget(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.FuncLit:
		return "", true
	case *ast.Ident:
		return e.Name, true
	case *ast.SelectorExpr:
		target := receiverName(e.X)
		if target == "" {
			return e.Sel.Name, true
		}
		return target + "." + e.Sel.Name, true
	default:
		return "", false
	}
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
