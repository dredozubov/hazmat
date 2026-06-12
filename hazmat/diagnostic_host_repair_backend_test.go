package hazmat

import (
	"os"
	"path/filepath"
	"sort"
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

func TestHostRepairBackendSupportsExecutableRegistryActions(t *testing.T) {
	var missingApply []string
	var missingVerify []string
	for id, action := range diagnosticRepairActionDefinitions {
		if id != action.ID {
			t.Fatalf("repair action registry key %q does not match definition ID %q", id, action.ID)
		}
		policy, ok := diagnosticRepairClassPolicyFor(action.Repairability)
		if !ok {
			t.Fatalf("%s has unknown repairability %q", action.ID, action.Repairability)
		}
		if !policy.ExecutableByHazmat {
			continue
		}
		if !diagnosticHostRepairBackendSupportsAction(action.ID) {
			missingApply = append(missingApply, string(action.ID))
		}
		if !diagnosticHostRepairBackendSupportsVerification(action.Verification) {
			missingVerify = append(missingVerify, string(action.Verification))
		}
	}
	if len(missingApply) > 0 {
		sort.Strings(missingApply)
		t.Fatalf("host repair backend missing apply dispatch for executable actions: %v", missingApply)
	}
	if len(missingVerify) > 0 {
		sort.Strings(missingVerify)
		t.Fatalf("host repair backend missing verification dispatch for executable actions: %v", missingVerify)
	}
}
