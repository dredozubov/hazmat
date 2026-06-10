package hazmat

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// The vendored copy exists so that pin bumps arrive as reviewable script
// diffs: the claude-installer-pin workflow updates both together, and this
// test refuses to let them drift apart.
func TestClaudeInstallerPinMatchesVendoredScript(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-install.sh")
	if err != nil {
		t.Fatalf("read vendored installer: %v", err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != claudeInstallerSHA256 {
		t.Fatalf("claudeInstallerSHA256 = %s but testdata/claude-install.sh hashes to %s; update both together (see .github/workflows/claude-installer-pin.yml)", claudeInstallerSHA256, got)
	}
}
