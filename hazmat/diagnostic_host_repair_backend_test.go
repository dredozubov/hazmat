package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticRepairEvidencePathsRequiresStructuredPrefixes(t *testing.T) {
	item := diagnosticRepairPlanItem{
		Details: []string{
			"/tmp/unprefixed",
			"path: /tmp/from-path",
			"workspace: /tmp/from-workspace",
			"fix with: sudo chmod 2770 /tmp/not-evidence",
			"path: relative/path",
		},
	}

	got := diagnosticRepairEvidencePaths(item, "path:", "workspace:")
	want := []string{"/tmp/from-path", "/tmp/from-workspace"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
}

func TestBoundedRepairDirsAcceptsOnlyRealDirsUnderRoot(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := boundedRepairDirs(root, []string{project, project})
	if err != nil {
		t.Fatalf("boundedRepairDirs: %v", err)
	}
	if len(paths) != 1 || paths[0] != project {
		t.Fatalf("paths = %#v, want [%q]", paths, project)
	}
}

func TestBoundedRepairDirsRejectsPathEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	for _, candidate := range []string{
		root,
		outside,
		link,
		filepath.Join(root, "..", filepath.Base(outside)),
		"relative",
	} {
		t.Run(candidate, func(t *testing.T) {
			if _, err := boundedRepairDirs(root, []string{candidate}); err == nil {
				t.Fatalf("boundedRepairDirs(%q) = nil error, want rejection", candidate)
			}
		})
	}
}

func TestUISetupOutputIsSuppressedInJSONMode(t *testing.T) {
	ui := &UI{JSON: true}
	out, err := captureStdout(t, func() error {
		ui.Step("hidden")
		ui.Ok("hidden")
		ui.SkipDone("hidden")
		ui.WarnMsg("hidden")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("JSON setup output = %q, want empty", out)
	}
}
