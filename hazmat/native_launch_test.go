package hazmat

import (
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
