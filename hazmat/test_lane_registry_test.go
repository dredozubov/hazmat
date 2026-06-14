package hazmat

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var knownTestLanes = map[string]bool{
	"source-safety":                true,
	"package-boundaries":           true,
	"package-contracts":            true,
	"os-linux":                     true,
	"os-macos":                     true,
	"cli-ux":                       true,
	"product-workflows":            true,
	"release-artifacts":            true,
	"tla-proof-hygiene":            true,
	"tla-model-check":              true,
	"privileged-install-ownership": true,
	"live-approved":                true,
	"destructive-lifecycle":        true,
	"drift":                        true,
}

func TestEveryEntrypointMapsToALane(t *testing.T) {
	registry := loadTestLaneRegistry(t)
	for entrypoint, lanes := range registry {
		if lanes.primary == "" {
			t.Fatalf("%s has empty primary lane", entrypoint)
		}
		assertKnownTestLane(t, entrypoint, lanes.primary)
		for _, lane := range lanes.secondary {
			assertKnownTestLane(t, entrypoint, lane)
		}
	}

	for _, script := range topLevelScriptEntrypoints(t) {
		if _, ok := registry[script]; !ok {
			t.Fatalf("%s is missing from docs/test-lanes.tsv", script)
		}
	}
	for _, job := range ciJobEntrypoints(t, "../.github/workflows/ci.yml") {
		if _, ok := registry[job]; !ok {
			t.Fatalf("%s is missing from docs/test-lanes.tsv", job)
		}
	}
}

type testLaneMapping struct {
	primary   string
	secondary []string
}

func loadTestLaneRegistry(t *testing.T) map[string]testLaneMapping {
	t.Helper()

	f, err := os.Open("../docs/test-lanes.tsv")
	if err != nil {
		t.Fatalf("open lane registry: %v", err)
	}
	defer f.Close()

	entries := make(map[string]testLaneMapping)
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("docs/test-lanes.tsv:%d: got %d columns, want 3", lineNo, len(cols))
		}
		entrypoint := cols[0]
		if entrypoint == "" {
			t.Fatalf("docs/test-lanes.tsv:%d: empty entrypoint", lineNo)
		}
		if _, dup := entries[entrypoint]; dup {
			t.Fatalf("docs/test-lanes.tsv:%d: duplicate entrypoint %s", lineNo, entrypoint)
		}
		entries[entrypoint] = testLaneMapping{
			primary:   cols[1],
			secondary: parseSecondaryLanes(cols[2]),
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan lane registry: %v", err)
	}
	return entries
}

func parseSecondaryLanes(raw string) []string {
	if raw == "-" || raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	lanes := make([]string, 0, len(parts))
	for _, part := range parts {
		lanes = append(lanes, strings.TrimSpace(part))
	}
	return lanes
}

func assertKnownTestLane(t *testing.T, entrypoint, lane string) {
	t.Helper()
	if !knownTestLanes[lane] {
		t.Fatalf("%s references unknown lane %q", entrypoint, lane)
	}
}

func topLevelScriptEntrypoints(t *testing.T) []string {
	t.Helper()

	dirents, err := os.ReadDir("../scripts")
	if err != nil {
		t.Fatalf("read scripts dir: %v", err)
	}
	var scripts []string
	for _, dirent := range dirents {
		if dirent.IsDir() || !strings.HasSuffix(dirent.Name(), ".sh") {
			continue
		}
		scripts = append(scripts, filepath.ToSlash(filepath.Join("scripts", dirent.Name())))
	}
	sort.Strings(scripts)
	return scripts
}

func ciJobEntrypoints(t *testing.T, workflowPath string) []string {
	t.Helper()

	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	jobsStart := strings.Index(string(data), "\njobs:\n")
	if jobsStart < 0 {
		t.Fatalf("%s has no jobs section", workflowPath)
	}
	data = data[jobsStart+len("\njobs:\n"):]
	jobName := regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):$`)
	var jobs []string
	for _, match := range jobName.FindAllSubmatch(data, -1) {
		name := string(match[1])
		jobs = append(jobs, ".github/workflows/ci.yml#job:"+name)
	}
	sort.Strings(jobs)
	return jobs
}
