package hazmat

import "fmt"

type rollbackReceiptStatus string

const (
	rollbackReceiptRemoved   rollbackReceiptStatus = "removed"
	rollbackReceiptPreserved rollbackReceiptStatus = "preserved"
	rollbackReceiptManual    rollbackReceiptStatus = "manual"
)

type rollbackReceipt struct {
	ResourceID       string
	ResourceOwner    string
	RollbackBoundary string
	Status           rollbackReceiptStatus
	Details          []string
}

func rollbackReceipts(deleteUser, deleteGroup bool, entries []credentialInventoryEntry) []rollbackReceipt {
	receipts := []rollbackReceipt{
		newRollbackReceipt("setup.sudoers", "setup.sudoers", rollbackReceiptRemoved, "removed Hazmat sudoers entries when present"),
		newRollbackReceipt("network.launchd-persistence", "setup.network-persistence", rollbackReceiptRemoved, "removed Hazmat LaunchDaemon persistence when present"),
		newRollbackReceipt("network.pf", "setup.network-pf", rollbackReceiptRemoved, "removed Hazmat pf anchor state when present"),
		newRollbackReceipt("network.dns-blocklist", "setup.network-dns-blocklist", rollbackReceiptRemoved, "removed Hazmat DNS blocklist entries when present"),
		newRollbackReceipt("setup.seatbelt-wrapper", "setup.seatbelt", rollbackReceiptRemoved, "removed Hazmat seatbelt wrapper state when present"),
		newRollbackReceipt("setup.host-wrappers", "setup.wrappers", rollbackReceiptRemoved, "removed host wrappers, completions, and managed Git safe.directory entries when present"),
		newRollbackReceipt("setup.home-traverse", "setup.workspace-permissions", rollbackReceiptRemoved, "removed managed home traversal ACLs when present"),
		newRollbackReceipt("agent-shell.umask", "setup.agent-shell", rollbackReceiptRemoved, "removed managed umask block when present"),
	}
	if deleteUser {
		receipts = append(receipts, newRollbackReceipt("setup.agent-user", "setup.agent-account", rollbackReceiptRemoved, fmt.Sprintf("deleted %s because --delete-user was supplied", agentUser)))
	} else {
		receipts = append(receipts, newRollbackReceipt("setup.agent-user", "setup.agent-account", rollbackReceiptPreserved, fmt.Sprintf("preserved %s; pass --delete-user for destructive account removal", agentUser)))
	}
	if deleteGroup {
		receipts = append(receipts, newRollbackReceipt("workspace.group-inheritance", "setup.workspace-permissions", rollbackReceiptRemoved, fmt.Sprintf("deleted %s because --delete-group was supplied", sharedGroup)))
	} else {
		receipts = append(receipts, newRollbackReceipt("workspace.group-inheritance", "setup.workspace-permissions", rollbackReceiptPreserved, fmt.Sprintf("preserved %s; pass --delete-group for destructive group removal", sharedGroup)))
	}
	receipts = append(receipts, rollbackCredentialReceipts(entries)...)
	return receipts
}

func rollbackCredentialReceipts(entries []credentialInventoryEntry) []rollbackReceipt {
	var receipts []rollbackReceipt
	for _, entry := range entries {
		resourceID := rollbackCredentialResourceID(entry)
		switch entry.Status() {
		case credentialInventoryConfigured:
			receipts = append(receipts, newRollbackReceipt(resourceID, "credentials.host-secret-store", rollbackReceiptPreserved, fmt.Sprintf("preserved host-owned credential store entry for %s", entry.ID)))
		case credentialInventoryNeedsRepair:
			receipts = append(receipts, newRollbackReceipt(resourceID, "credentials.host-secret-store", rollbackReceiptManual, rollbackCredentialDetails(entry)...))
		case credentialInventoryAdapterRequired:
			receipts = append(receipts, newRollbackReceipt("credential.backend-adapter", "credentials.host-secret-store", rollbackReceiptManual, fmt.Sprintf("%s still needs a credential backend adapter; rollback did not claim this cleanup", entry.ID)))
		case credentialInventoryExternal:
			receipts = append(receipts, newRollbackReceipt(resourceID, "credentials.host-secret-store", rollbackReceiptManual, fmt.Sprintf("%s is external credential state; rollback did not claim this cleanup", entry.ID)))
		case credentialInventoryError:
			receipts = append(receipts, newRollbackReceipt(resourceID, "credentials.host-secret-store", rollbackReceiptManual, append([]string{fmt.Sprintf("credential inventory for %s had errors; rollback left it for manual inspection", entry.ID)}, entry.Errors...)...))
		case credentialInventoryNotConfigured:
			continue
		}
	}
	return receipts
}

func rollbackCredentialDetails(entry credentialInventoryEntry) []string {
	details := []string{fmt.Sprintf("preserved credential residue for %s; preview supported repairs with hazmat doctor --dry-run, apply them with hazmat doctor --fix, or run hazmat config migration before cleanup", entry.ID)}
	for _, finding := range entry.AgentResidue {
		details = append(details, fmt.Sprintf("agent residue: %s", finding.Path))
	}
	for _, finding := range entry.LegacyResidue {
		details = append(details, fmt.Sprintf("legacy residue: %s", finding.Path))
	}
	return details
}

func rollbackCredentialResourceID(entry credentialInventoryEntry) string {
	switch entry.ID {
	case credentialHarnessClaudeState:
		return "credential.claude-state"
	case credentialCloudS3SecretKey:
		return "credential.cloud-secret-key"
	default:
		return "credential.residue"
	}
}

func newRollbackReceipt(resourceID, boundary string, status rollbackReceiptStatus, details ...string) rollbackReceipt {
	return rollbackReceipt{
		ResourceID:       resourceID,
		ResourceOwner:    diagnosticRepairResourceOwner(resourceID),
		RollbackBoundary: boundary,
		Status:           status,
		Details:          append([]string(nil), details...),
	}
}

func printRollbackReceipts(receipts []rollbackReceipt) {
	if len(receipts) == 0 {
		return
	}
	fmt.Println()
	cBold.Println("━━━ Rollback receipts ━━━")
	fmt.Println()
	for _, receipt := range receipts {
		fmt.Printf("  [%s] %s\n", receipt.Status, receipt.ResourceID)
		if receipt.ResourceOwner != "" {
			cDim.Printf("     Owner: %s\n", receipt.ResourceOwner)
		}
		if receipt.RollbackBoundary != "" {
			cDim.Printf("     Boundary: %s\n", receipt.RollbackBoundary)
		}
		for _, detail := range receipt.Details {
			cDim.Printf("     Detail: %s\n", detail)
		}
	}
}
