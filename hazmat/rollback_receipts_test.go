package hazmat

import (
	"strings"
	"testing"
)

func TestRollbackReceiptsClassifyRemovedPreservedAndManualItems(t *testing.T) {
	receipts := rollbackReceipts(false, false, []credentialInventoryEntry{
		{
			ID:               credentialHarnessClaudeState,
			Backend:          credentialStorageHostSecretStore,
			HostStorePresent: true,
		},
		{
			ID: credentialCloudS3SecretKey,
			LegacyResidue: []credentialInventoryFinding{{
				Path:   "/Users/test/.hazmat/cloud.yaml",
				Detail: "legacy cloud secret",
			}},
		},
		{
			ID:      credentialHarnessAntigravityKeychain,
			Support: credentialSupportAdapterRequired,
		},
	})

	assertRollbackReceipt(t, receipts, "network.pf", rollbackReceiptRemoved)
	assertRollbackReceipt(t, receipts, "setup.agent-user", rollbackReceiptPreserved)
	assertRollbackReceipt(t, receipts, "workspace.group-inheritance", rollbackReceiptPreserved)
	assertRollbackReceipt(t, receipts, "credential.claude-state", rollbackReceiptPreserved)
	assertRollbackReceipt(t, receipts, "credential.cloud-secret-key", rollbackReceiptManual)
	assertRollbackReceipt(t, receipts, "credential.backend-adapter", rollbackReceiptManual)
}

func TestRollbackReceiptsRespectDestructiveFlags(t *testing.T) {
	receipts := rollbackReceipts(true, true, nil)

	assertRollbackReceipt(t, receipts, "setup.agent-user", rollbackReceiptRemoved)
	assertRollbackReceipt(t, receipts, "workspace.group-inheritance", rollbackReceiptRemoved)
}

func TestRollbackCredentialReceiptsIncludeResiduePaths(t *testing.T) {
	receipts := rollbackCredentialReceipts([]credentialInventoryEntry{
		{
			ID: credentialProviderOpenAIAPIKey,
			AgentResidue: []credentialInventoryFinding{{
				Path:   "/Users/agent/.openai",
				Detail: "stale agent credential",
			}},
		},
	})

	receipt := findRollbackReceipt(receipts, "credential.residue", rollbackReceiptManual)
	if receipt == nil {
		t.Fatalf("receipts = %+v, want credential residue manual receipt", receipts)
	}
	if !containsPlanString(receipt.Details, "agent residue: /Users/agent/.openai") {
		t.Fatalf("receipt details = %+v, want agent residue path", receipt.Details)
	}
}

func TestRollbackHelpNamesReceiptAndPreservationBoundary(t *testing.T) {
	cmd := newRollbackCmd()
	help := cmd.Long
	normalized := strings.Join(strings.Fields(help), " ")
	for _, want := range []string{
		"Rollback prints receipts",
		"removed, preserved, and manual follow-up items",
		"Host-owned credential stores",
		"session-time permission repairs",
		"Use --dry-run to preview",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("rollback help = %q, want %q", help, want)
		}
	}
}

func assertRollbackReceipt(t *testing.T, receipts []rollbackReceipt, resourceID string, status rollbackReceiptStatus) {
	t.Helper()
	receipt := findRollbackReceipt(receipts, resourceID, status)
	if receipt == nil {
		t.Fatalf("receipts = %+v, want %s/%s", receipts, resourceID, status)
	}
	if receipt.RollbackBoundary == "" {
		t.Fatalf("receipt = %+v, want rollback boundary", *receipt)
	}
}

func findRollbackReceipt(receipts []rollbackReceipt, resourceID string, status rollbackReceiptStatus) *rollbackReceipt {
	for i := range receipts {
		if receipts[i].ResourceID == resourceID && receipts[i].Status == status {
			return &receipts[i]
		}
	}
	return nil
}
