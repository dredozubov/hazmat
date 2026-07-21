package hazmat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type listedPackage struct {
	ImportPath string   `json:"ImportPath"`
	Dir        string   `json:"Dir"`
	Standard   bool     `json:"Standard"`
	Imports    []string `json:"Imports"`
	Deps       []string `json:"Deps"`
	GoFiles    []string `json:"GoFiles"`
}

var reusableCorePackages = []string{
	"hazmat/sessionrequest",
	"hazmat/pathpolicy",
	"hazmat/sessionplanner",
	"hazmat/sessioncontract",
	"hazmat/containment",
	"hazmat/sessionbackend",
	"hazmat/credentials",
	"hazmat/harnesses",
	"hazmat/integrations",
	"hazmat/runtimeprovider",
	"hazmat/runtimeauthority",
	"hazmat/runtimecapability",
	"hazmat/planescapeprovider",

	// Current reusable leaves that are already enforced by the package-split
	// guard and remain effect-free under the same dependency rules.
	"hazmat/attestationtier",
	"hazmat/hostbroker",
	"hazmat/containment/applecontainer",
	"hazmat/containment/darwin",
	"hazmat/containment/docker",
	"hazmat/containment/linux",
	"hazmat/configmodel",
	"hazmat/hostfacts",
	"hazmat/sessionmeta",
	"hazmat/internal/sessionflow",
}

var reusableCoreForbiddenDeps = []string{
	"os/exec",
	"net/http",
	"github.com/spf13/cobra",
	"golang.org/x/term",
	"hazmat/cmd/",
	"hazmat/internal/",
}

func TestImportBoundaries(t *testing.T) {
	pkgs := loadListedPackages(t)

	for _, importPath := range reusableCorePackages {
		pkg, ok := pkgs[importPath]
		if !ok {
			t.Fatalf("pure package %s is not listed by go list", importPath)
		}
		assertNoForbiddenDeps(t, pkg, reusableCoreForbiddenDeps)
		assertNoRuntimeGOOS(t, pkg)
	}

	compilerPackages := map[string]bool{
		"hazmat/containment/applecontainer": true,
		"hazmat/containment/darwin":         true,
		"hazmat/containment/docker":         true,
		"hazmat/containment/linux":          true,
	}
	for compiler := range compilerPackages {
		pkg, ok := pkgs[compiler]
		if !ok {
			t.Fatalf("compiler package %s is not listed by go list", compiler)
		}
		assertNoForbiddenDeps(t, pkg, []string{
			"hazmat/cmd/hazmat-launch",
			"hazmat/internal/runtime/",
		})
	}

	if pkg, ok := pkgs["hazmat/containment"]; ok {
		assertNoForbiddenDeps(t, pkg, []string{
			"hazmat/containment/applecontainer",
			"hazmat/containment/darwin",
			"hazmat/containment/docker",
			"hazmat/containment/linux",
		})
	}
	for importPath := range pkgs {
		if !strings.HasPrefix(importPath, "hazmat/containment/") {
			continue
		}
		if !compilerPackages[importPath] {
			t.Fatalf("containment backend package %s is not classified in TestImportBoundaries", importPath)
		}
	}

	for _, descriptor := range []string{
		"hazmat/credentials",
	} {
		pkg, ok := pkgs[descriptor]
		if !ok {
			continue
		}
		assertNoForbiddenDeps(t, pkg, []string{
			"hazmat/internal/credentialruntime",
		})
	}

	if pkg, ok := pkgs["hazmat/planescapeprovider"]; ok {
		// The shared-provider client is a protocol peer, never a second Linux
		// containment compiler or a route to an in-process fallback.
		assertNoForbiddenDeps(t, pkg, []string{
			"hazmat/containment",
			"hazmat/containment/linux",
		})
	}

	for importPath, pkg := range pkgs {
		if importPath == "hazmat" ||
			strings.HasPrefix(importPath, "hazmat/cmd/") ||
			strings.HasPrefix(importPath, "hazmat/internal/frontend/") {
			continue
		}
		assertNoForbiddenDeps(t, pkg, []string{
			"hazmat/internal/frontend/",
			"hazmat/cmd/hazmat",
		})
	}

	// Arch B: the Hazmat binary (every package, including cmd/) must never link
	// the Beadpost root module or Dolt. The shared wire contract
	// (local/beadpost-contracts) is allowed ONLY in the beadpost_hostbroker build;
	// the default/public build must not depend on it either.
	forbidden := []string{"beadpost", "github.com/dolthub"}
	if !hostbrokerEnabled {
		forbidden = append(forbidden, "local/beadpost-contracts")
	}
	for _, pkg := range pkgs {
		assertNoForbiddenDeps(t, pkg, forbidden)
	}
}

func TestFakeExternalConsumerImportsReusableCore(t *testing.T) {
	dir := writeExternalConsumerModule(t, map[string]string{
		"consumer.go": `package externalconsumer

import (
	"fmt"

	"hazmat/containment"
	linuxspec "hazmat/containment/linux"
	"hazmat/hostfacts"
	platformlinux "hazmat/platform/linux"
	"hazmat/runtimeprovider"
	"hazmat/sessionbackend"
	"hazmat/sessionmeta"
)

func BuildReusableCorePlan() error {
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{{Path: "/home/agent/.ssh"}})
	if err != nil {
		return err
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:      containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{"/opt/sdk"}, containment.PathReadOnly),
		AgentHome: containment.AgentHomePolicy{
			Path: "/tmp/hazmat-session/home",
			Mode: containment.AgentHomeModeSessionLocal,
			PersistentPath: "/home/agent",
		},
		Temp: containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network: containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process: containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		return err
	}
	report := platformlinux.Report{
		RuntimeOS: "linux",
		Features: platformlinux.FeatureSet{
			UserNamespaces: available(),
			MountNamespaces: available(),
			CgroupV2: available(),
			Landlock: available(),
			Seccomp: available(),
			NetworkNamespaces: available(),
		},
	}
	spec, err := linuxspec.Compile(contract, linuxspec.CompileOptions{
		Platform: report,
		Identity: linuxspec.IdentityCurrentUser,
		HelperStrategy: linuxspec.HelperRootlessUserNS,
	})
	if err != nil {
		return err
	}
	if spec.Identity != linuxspec.IdentityCurrentUser || spec.HelperStrategy != linuxspec.HelperRootlessUserNS {
		return fmt.Errorf("linux spec identity/helper = %q/%q", spec.Identity, spec.HelperStrategy)
	}
	plan := sessionbackend.BuildPlan(sessionbackend.Input{
		Target: "codex",
		Mode: sessionmeta.ModeNative,
		ProjectDir: contract.ProjectPath(),
		NetworkMode: sessionmeta.NetworkNone,
		HostFacts: hostfacts.ForGOOS("linux"),
	})
	prepared, err := sessionbackend.NewPreparedLaunch(
		plan,
		sessionbackend.NewLinuxLaunchArtifact(sessionbackend.LinuxLaunchSpec{
			FormatVersion: spec.FormatVersion,
			Backend: spec.Backend,
			Phase: spec.Phase,
		}),
		[]sessionbackend.AcceptedGap{{
			Feature: sessionbackend.GapNativeLaunch,
			Justification: "external consumer accepts plan-only Linux preview",
		}},
	)
	if err != nil {
		return err
	}
	if prepared.ArtifactKind() != sessionbackend.PreparedArtifactLinuxLaunch {
		return fmt.Errorf("artifact kind = %q", prepared.ArtifactKind())
	}
	var record runtimeprovider.ProviderStatusRecord
	for _, descriptor := range runtimeprovider.KnownDescriptors() {
		if descriptor.Kind == runtimeprovider.KindLinuxCurrentUser {
			record = descriptor.StatusRecord()
			break
		}
	}
	if record.Status != runtimeprovider.StatusPlanOnly || record.Executable {
		return fmt.Errorf("linux current-user record = %+v, want non-executable plan-only", record)
	}
	return nil
}

func available() platformlinux.FeatureReport {
	return platformlinux.FeatureReport{State: platformlinux.FeatureAvailable, Source: "external-consumer-test"}
}
`,
		"consumer_test.go": `package externalconsumer

import "testing"

func TestBuildReusableCorePlan(t *testing.T) {
	if err := BuildReusableCorePlan(); err != nil {
		t.Fatal(err)
	}
}
`,
	})
	runExternalGoCommand(t, dir, "test", "./...")

	pkgs := loadExternalConsumerPackages(t, dir)
	requiredDeps := []string{
		"hazmat/containment",
		"hazmat/containment/linux",
		"hazmat/platform/linux",
		"hazmat/runtimeprovider",
		"hazmat/sessionbackend",
	}
	consumer, ok := pkgs["externalconsumer"]
	if !ok {
		t.Fatalf("external consumer package is missing from go list output")
	}
	if missing := missingDeps(consumer, requiredDeps); len(missing) > 0 {
		t.Fatalf("external consumer graph missing required deps: %s", strings.Join(missing, ", "))
	}
	forbidden := []string{
		"os/exec",
		"net/http",
		"github.com/spf13/cobra",
		"golang.org/x/term",
		"hazmat/cmd/",
		"hazmat/internal/",
	}
	if violations := forbiddenDepViolations(consumer, forbidden); len(violations) > 0 {
		t.Fatalf("external consumer imports host-effect dependencies: %s", strings.Join(violations, ", "))
	}
}

func TestFakeExternalConsumerCannotImportInternalRuntime(t *testing.T) {
	dir := writeExternalConsumerModule(t, map[string]string{
		"consumer_test.go": `package externalconsumer

import (
	"testing"

	_ "hazmat/internal/runtime/linux"
)

func TestInternalRuntimeImport(t *testing.T) {}
`,
	})
	out, err := runExternalGoCommandError(dir, "test", "./...")
	if err == nil {
		t.Fatal("external consumer imported hazmat/internal/runtime/linux")
	}
	if !strings.Contains(out, "use of internal package hazmat/internal/runtime/linux not allowed") {
		t.Fatalf("unexpected external consumer import failure:\n%s", out)
	}
}

func TestPackageSplitDependencyGraph(t *testing.T) {
	graph := loadPackageSplitGraph(t)
	if len(graph.nodes) == 0 {
		t.Fatal("package split dependency graph has no nodes")
	}

	for _, edge := range graph.edges {
		if _, ok := graph.nodes[edge.from]; !ok {
			t.Fatalf("dependency graph edge references undefined source %q", edge.from)
		}
		if _, ok := graph.nodes[edge.to]; !ok {
			t.Fatalf("dependency graph edge references undefined target %q", edge.to)
		}
		if graph.groups[edge.from] == "contracts" && graph.groups[edge.to] == "runtimes" {
			t.Fatalf("dependency graph has forbidden contract-to-runtime edge %s --> %s", edge.from, edge.to)
		}
	}

	if cycle := graphCycle(graph); len(cycle) > 0 {
		t.Fatalf("dependency graph contains cycle: %s", strings.Join(cycle, " -> "))
	}
}

func loadListedPackages(t *testing.T) map[string]listedPackage {
	t.Helper()

	args := []string{"list", "-deps", "-json"}
	if hostbrokerEnabled {
		// Resolve the dependency graph for the same build the test runs under, so
		// the tagged build's local/beadpost-contracts edge is visible (and checked).
		args = append(args, "-tags", "beadpost_hostbroker")
	}
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %v: %v\n%s", args, err, stderr.String())
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	pkgs := make(map[string]listedPackage)
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode go list output: %v", err)
		}
		if pkg.Standard || !strings.HasPrefix(pkg.ImportPath, "hazmat") {
			continue
		}
		pkgs[pkg.ImportPath] = pkg
	}
	return pkgs
}

func writeExternalConsumerModule(t *testing.T, files map[string]string) string {
	t.Helper()

	moduleDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := fmt.Sprintf(`module externalconsumer

go 1.25.0

require hazmat v0.0.0

replace hazmat => %s
`, moduleDir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func loadExternalConsumerPackages(t *testing.T, dir string) map[string]listedPackage {
	t.Helper()

	out := runExternalGoCommand(t, dir, "list", "-deps", "-json", ".")
	dec := json.NewDecoder(bytes.NewReader(out))
	pkgs := make(map[string]listedPackage)
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode external go list output: %v", err)
		}
		if pkg.Standard {
			continue
		}
		pkgs[pkg.ImportPath] = pkg
	}
	return pkgs
}

func missingDeps(pkg listedPackage, required []string) []string {
	deps := depSet(pkg.Deps)
	var missing []string
	for _, requiredDep := range required {
		if !deps[requiredDep] {
			missing = append(missing, requiredDep)
		}
	}
	return missing
}

func runExternalGoCommand(t *testing.T, dir string, args ...string) []byte {
	t.Helper()

	out, err := runExternalGoCommandError(dir, args...)
	if err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
	return []byte(out)
}

func runExternalGoCommandError(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func depSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func assertNoForbiddenDeps(t *testing.T, pkg listedPackage, forbidden []string) {
	t.Helper()

	violations := forbiddenDepViolations(pkg, forbidden)
	if len(violations) > 0 {
		t.Fatalf("%s imports forbidden dependencies: %s", pkg.ImportPath, strings.Join(violations, ", "))
	}
}

func forbiddenDepViolations(pkg listedPackage, forbidden []string) []string {
	var violations []string
	for _, dep := range pkg.Deps {
		for _, denied := range forbidden {
			if dep == strings.TrimSuffix(denied, "/") || strings.HasPrefix(dep, denied) {
				violations = append(violations, dep)
			}
		}
	}
	sort.Strings(violations)
	return violations
}

func TestForbiddenDependencyMatcher(t *testing.T) {
	pkg := listedPackage{
		ImportPath: "hazmat/sessionrequest",
		Deps: []string{
			"hazmat/containment",
			"hazmat/internality",
			"hazmat/internal/setup",
			"hazmat/internal/runtime/linux",
			"github.com/spf13/cobra",
			"os/exec",
		},
	}
	got := forbiddenDepViolations(pkg, reusableCoreForbiddenDeps)
	want := []string{
		"github.com/spf13/cobra",
		"hazmat/internal/runtime/linux",
		"hazmat/internal/setup",
		"os/exec",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("forbidden dependency matcher = %#v, want %#v", got, want)
	}
}

func assertNoRuntimeGOOS(t *testing.T, pkg listedPackage) {
	t.Helper()

	for _, name := range pkg.GoFiles {
		path := filepath.Join(pkg.Dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		runtimeNames := runtimeImportNames(file)
		if len(runtimeNames) == 0 {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch expr := node.(type) {
			case *ast.SelectorExpr:
				x, ok := expr.X.(*ast.Ident)
				if ok && runtimeNames[x.Name] && expr.Sel.Name == "GOOS" {
					t.Fatalf("%s reads runtime.GOOS in %s", pkg.ImportPath, path)
				}
			case *ast.Ident:
				if runtimeNames["."] && expr.Name == "GOOS" {
					t.Fatalf("%s reads runtime.GOOS via dot import in %s", pkg.ImportPath, path)
				}
			}
			return true
		})
	}
}

func runtimeImportNames(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != "runtime" {
			continue
		}
		if imp.Name == nil {
			names["runtime"] = true
			continue
		}
		names[imp.Name.Name] = true
	}
	return names
}

type mermaidGraph struct {
	nodes  map[string]bool
	groups map[string]string
	edges  []mermaidEdge
}

type mermaidEdge struct {
	from string
	to   string
}

func loadPackageSplitGraph(t *testing.T) mermaidGraph {
	t.Helper()

	path := filepath.Join("..", "docs", "plans", "2026-06-02-package-split-architecture.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read package split architecture doc: %v", err)
	}
	text := string(raw)
	start := strings.Index(text, "## Dependency Graph")
	if start < 0 {
		t.Fatalf("%s does not contain dependency graph heading", path)
	}
	blockStart := strings.Index(text[start:], "```mermaid")
	if blockStart < 0 {
		t.Fatalf("%s dependency graph has no mermaid block", path)
	}
	blockStart += start + len("```mermaid")
	blockEnd := strings.Index(text[blockStart:], "```")
	if blockEnd < 0 {
		t.Fatalf("%s dependency graph mermaid block is unterminated", path)
	}
	block := text[blockStart : blockStart+blockEnd]

	graph := mermaidGraph{
		nodes:  make(map[string]bool),
		groups: make(map[string]string),
	}
	groupStack := []string{}
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "subgraph ") {
			groupStack = append(groupStack, parseSubgraphName(trimmed))
			continue
		}
		if trimmed == "end" {
			if len(groupStack) > 0 {
				groupStack = groupStack[:len(groupStack)-1]
			}
			continue
		}
		if name := parseMermaidNode(trimmed); name != "" {
			graph.nodes[name] = true
			if len(groupStack) > 0 {
				graph.groups[name] = groupStack[len(groupStack)-1]
			}
			continue
		}
		if edge := parseMermaidEdge(trimmed); edge.from != "" {
			graph.edges = append(graph.edges, edge)
		}
	}
	return graph
}

func parseSubgraphName(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "subgraph "))
	if idx := strings.Index(rest, "["); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

func parseMermaidNode(line string) string {
	idx := strings.Index(line, "[")
	if idx <= 0 || strings.Contains(line[:idx], " ") {
		return ""
	}
	name := line[:idx]
	if !validMermaidIdentifier(name) {
		return ""
	}
	return name
}

func parseMermaidEdge(line string) mermaidEdge {
	parts := strings.Split(line, "-->")
	if len(parts) != 2 {
		return mermaidEdge{}
	}
	from := strings.TrimSpace(parts[0])
	to := strings.TrimSpace(parts[1])
	if !validMermaidIdentifier(from) || !validMermaidIdentifier(to) {
		return mermaidEdge{}
	}
	return mermaidEdge{from: from, to: to}
}

func validMermaidIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func graphCycle(graph mermaidGraph) []string {
	adj := make(map[string][]string)
	for _, edge := range graph.edges {
		adj[edge.from] = append(adj[edge.from], edge.to)
	}
	for node := range adj {
		sort.Strings(adj[node])
	}

	var stack []string
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string) []string
	visit = func(node string) []string {
		if visiting[node] {
			for i, item := range stack {
				if item == node {
					return append(append([]string{}, stack[i:]...), node)
				}
			}
			return []string{node, node}
		}
		if visited[node] {
			return nil
		}
		visiting[node] = true
		stack = append(stack, node)
		for _, next := range adj[node] {
			if cycle := visit(next); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		visiting[node] = false
		visited[node] = true
		return nil
	}

	nodes := make([]string, 0, len(graph.nodes))
	for node := range graph.nodes {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	for _, node := range nodes {
		if cycle := visit(node); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}

func TestImportBoundaryScript(t *testing.T) {
	script := filepath.Join("..", "scripts", "check-import-boundaries.sh")
	info, err := os.Stat(script)
	if err != nil {
		t.Fatalf("stat %s: %v", script, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("%s is not executable", script)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", script)
	}
}
