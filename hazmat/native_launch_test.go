package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReaderContainsWithinFindsMarkerAcrossChunkBoundary(t *testing.T) {
	marker := []byte("--hazmat-direct-exec")
	firstRead := 64*1024 + len(marker) - 1
	prefixLen := firstRead - 3
	input := strings.NewReader(strings.Repeat("x", prefixLen) + string(marker) + "suffix")

	if !readerContainsWithin(input, marker, int64(prefixLen+len(marker)+6)) {
		t.Fatal("readerContainsWithin() = false, want true for marker crossing chunk boundary")
	}
}

func TestReaderContainsWithinHonorsLimit(t *testing.T) {
	marker := []byte("--hazmat-direct-exec")
	limit := int64(128)
	input := strings.NewReader(strings.Repeat("x", int(limit)) + string(marker))

	if readerContainsWithin(input, marker, limit) {
		t.Fatal("readerContainsWithin() = true past limit, want false")
	}
}

func TestReaderContainsWithinRejectsMissingMarker(t *testing.T) {
	marker := []byte("--hazmat-direct-exec")
	input := strings.NewReader(strings.Repeat("x", 4096))

	if readerContainsWithin(input, marker, 4096) {
		t.Fatal("readerContainsWithin() = true for missing marker, want false")
	}
}

func TestReaderMarkersWithinFindsBothLaunchHelperCapabilities(t *testing.T) {
	markers := map[string][]byte{
		"direct_exec":  []byte("--hazmat-direct-exec"),
		"session_temp": []byte("--hazmat-session-temp"),
	}
	input := strings.NewReader(strings.Repeat("x", 127) + "--hazmat-session-temp" + strings.Repeat("y", 127) + "--hazmat-direct-exec")

	got := readerMarkersWithin(input, markers, 1024)
	for name := range markers {
		if !got[name] {
			t.Fatalf("readerMarkersWithin()[%s] = false, want true; got %#v", name, got)
		}
	}
}

func TestReadLaunchHelperCapabilitiesUsesBoundedMarkerScan(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "hazmat-launch")
	content := strings.Join([]string{
		"prefix",
		"--hazmat-session-temp",
		"--hazmat-direct-exec",
	}, "\x00")
	if err := os.WriteFile(helper, []byte(content), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	got := readLaunchHelperCapabilities(helper)
	if !got.DirectExec || !got.SessionTemp {
		t.Fatalf("readLaunchHelperCapabilities() = %+v, want both capabilities", got)
	}
}
