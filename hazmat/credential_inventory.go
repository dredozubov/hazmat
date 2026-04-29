package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type credentialInventoryStatus string

const (
	credentialInventoryConfigured      credentialInventoryStatus = "configured"
	credentialInventoryNotConfigured   credentialInventoryStatus = "not-configured"
	credentialInventoryExternal        credentialInventoryStatus = "external"
	credentialInventoryAdapterRequired credentialInventoryStatus = "adapter-required"
	credentialInventoryNeedsRepair     credentialInventoryStatus = "needs-repair"
	credentialInventoryError           credentialInventoryStatus = "error"
)

type credentialInventoryFinding struct {
	Path   string
	Detail string
	Repair string
}

type credentialInventoryEntry struct {
	ID               credentialID
	DisplayName      string
	Kind             credentialKind
	Backend          credentialStorageBackend
	Delivery         credentialDeliveryMode
	Support          credentialSupportStatus
	HostStorePresent bool
	StorePath        string
	AgentResidue     []credentialInventoryFinding
	LegacyResidue    []credentialInventoryFinding
	Errors           []string
}

type credentialInventorySummary struct {
	Total                  int
	ManagedHostSecretStore int
	Configured             int
	NotConfigured          int
	External               int
	AdapterRequired        int
	NeedsRepair            int
	Errors                 int
}

var (
	credentialInventoryPathExists = credentialInventoryPathExistsOnDisk
	credentialInventoryReadFile   = os.ReadFile
)

func inspectCredentialInventory(home string) ([]credentialInventoryEntry, error) {
	if strings.TrimSpace(home) == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home directory for credential inventory: %w", err)
		}
	}

	legacyCloud, legacyCloudErr := inspectLegacyCloudCredentialConfig()
	descriptors := builtinCredentialDescriptors()
	entries := make([]credentialInventoryEntry, 0, len(descriptors))

	for _, descriptor := range descriptors {
		entry := credentialInventoryEntry{
			ID:          descriptor.ID,
			DisplayName: descriptor.DisplayName,
			Kind:        descriptor.Kind,
			Backend:     descriptor.Backend,
			Delivery:    descriptor.Delivery,
			Support:     descriptor.Support,
		}

		if descriptor.Backend == credentialStorageHostSecretStore {
			storePath, err := descriptor.StorePathForHome(home)
			if err != nil {
				entry.Errors = append(entry.Errors, err.Error())
			} else {
				entry.StorePath = storePath
				if exists, err := credentialInventoryPathExists(storePath); err != nil {
					entry.Errors = append(entry.Errors, fmt.Sprintf("inspect host store path %s: %v", storePath, err))
				} else {
					entry.HostStorePresent = exists
				}
			}
		}

		agentResidue, agentErrors := inspectDescriptorAgentResidue(descriptor)
		entry.AgentResidue = append(entry.AgentResidue, agentResidue...)
		entry.Errors = append(entry.Errors, agentErrors...)
		legacy, errs := inspectDescriptorLegacyResidue(descriptor, legacyCloud)
		entry.LegacyResidue = append(entry.LegacyResidue, legacy...)
		entry.Errors = append(entry.Errors, errs...)
		if legacyCloudErr != nil && descriptor.Kind == credentialKindCloudBackup {
			entry.Errors = append(entry.Errors, fmt.Sprintf("inspect legacy cloud config %s: %v", configFilePath, legacyCloudErr))
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func summarizeCredentialInventory(entries []credentialInventoryEntry) credentialInventorySummary {
	summary := credentialInventorySummary{Total: len(entries)}
	for _, entry := range entries {
		if entry.Support == credentialSupportManaged && entry.Backend == credentialStorageHostSecretStore {
			summary.ManagedHostSecretStore++
		}
		switch entry.Status() {
		case credentialInventoryConfigured:
			summary.Configured++
		case credentialInventoryNotConfigured:
			summary.NotConfigured++
		case credentialInventoryExternal:
			summary.External++
		case credentialInventoryAdapterRequired:
			summary.AdapterRequired++
		case credentialInventoryNeedsRepair:
			summary.NeedsRepair++
		case credentialInventoryError:
			summary.Errors++
		}
	}
	return summary
}

func (entry credentialInventoryEntry) Status() credentialInventoryStatus {
	switch {
	case len(entry.Errors) > 0:
		return credentialInventoryError
	case len(entry.AgentResidue) > 0 || len(entry.LegacyResidue) > 0:
		return credentialInventoryNeedsRepair
	case entry.Support == credentialSupportAdapterRequired:
		return credentialInventoryAdapterRequired
	case entry.Support == credentialSupportExternal:
		return credentialInventoryExternal
	case entry.Backend == credentialStorageHostSecretStore && entry.HostStorePresent:
		return credentialInventoryConfigured
	default:
		return credentialInventoryNotConfigured
	}
}

func (entry credentialInventoryEntry) RepairHints() []string {
	seen := make(map[string]struct{})
	var hints []string
	for _, finding := range append(append([]credentialInventoryFinding{}, entry.AgentResidue...), entry.LegacyResidue...) {
		if finding.Repair == "" {
			continue
		}
		if _, ok := seen[finding.Repair]; ok {
			continue
		}
		seen[finding.Repair] = struct{}{}
		hints = append(hints, finding.Repair)
	}
	return hints
}

func formatCredentialInventoryEntry(entry credentialInventoryEntry) string {
	parts := []string{
		fmt.Sprintf("%s: %s", entry.ID, entry.Status()),
		fmt.Sprintf("backend=%s", entry.Backend),
		fmt.Sprintf("delivery=%s", entry.Delivery),
	}
	if entry.Backend == credentialStorageHostSecretStore {
		storeState := "absent"
		if entry.HostStorePresent {
			storeState = "present"
		}
		parts = append(parts, "host-store="+storeState)
	}
	return "Credential " + strings.Join(parts, " ")
}

func formatCredentialInventoryFinding(prefix string, finding credentialInventoryFinding) string {
	msg := fmt.Sprintf("%s: %s at %s", prefix, finding.Detail, finding.Path)
	if finding.Repair != "" {
		msg += " — " + finding.Repair
	}
	return msg
}

func inspectDescriptorAgentResidue(descriptor credentialDescriptor) ([]credentialInventoryFinding, []string) {
	switch descriptor.Delivery {
	case credentialDeliveryMaterializedFile:
		if descriptor.AgentPath == "" {
			return nil, nil
		}
		if exists, err := credentialInventoryPathExists(descriptor.AgentPath); err != nil {
			return nil, []string{fmt.Sprintf("inspect agent credential path %s: %v", descriptor.AgentPath, err)}
		} else if exists {
			return []credentialInventoryFinding{{
				Path:   descriptor.AgentPath,
				Detail: "stale agent-home materialized credential",
				Repair: "launch the matching harness once so Hazmat can harvest the file back into ~/.hazmat/secrets, or remove the stale file after verifying the host store",
			}}, nil
		}
	case credentialDeliveryBrokeredHelper:
		if descriptor.ID == credentialGitHTTPSAgentStore {
			if exists, err := credentialInventoryPathExists(gitHTTPSAgentCredentialsPath); err != nil {
				return nil, []string{fmt.Sprintf("inspect Git HTTPS credential path %s: %v", gitHTTPSAgentCredentialsPath, err)}
			} else if exists {
				return []credentialInventoryFinding{{
					Path:   gitHTTPSAgentCredentialsPath,
					Detail: "legacy agent-home Git HTTPS credential store",
					Repair: "launch a native Hazmat session once to migrate the Git HTTPS credentials into ~/.hazmat/secrets/git-https/credentials, or rotate and remove old PATs from the agent store",
				}}, nil
			}
		}
	case credentialDeliveryNone, credentialDeliveryEnv, credentialDeliveryExternalReference:
	}
	return nil, nil
}

func inspectDescriptorLegacyResidue(descriptor credentialDescriptor, cloud legacyCloudCredentialConfig) ([]credentialInventoryFinding, []string) {
	var findings []credentialInventoryFinding
	var errors []string

	if descriptor.Kind == credentialKindProviderAPIKey && descriptor.EnvVar != "" {
		finding, err := inspectLegacyProviderExport(descriptor.EnvVar)
		if err != nil {
			errors = append(errors, err.Error())
		} else if finding != nil {
			findings = append(findings, *finding)
		}
	}

	switch descriptor.ID {
	case credentialGitHTTPSAgentStore:
		if hasLegacyGitHTTPSCredentialHelper(gitHTTPSAgentGitConfigPath) {
			findings = append(findings, credentialInventoryFinding{
				Path:   gitHTTPSAgentGitConfigPath,
				Detail: "legacy persistent Git HTTPS credential helper",
				Repair: "run `hazmat config agent` to remove the persistent helper; Hazmat now injects a brokered helper only while a native session runs",
			})
		}
	case credentialGitSSHProvisionedIdentity:
		legacyRoot := legacyProvisionedSSHKeysRootDir()
		if exists, err := credentialInventoryPathExists(legacyRoot); err != nil {
			errors = append(errors, fmt.Sprintf("inspect legacy Git SSH key root %s: %v", legacyRoot, err))
		} else if exists {
			findings = append(findings, credentialInventoryFinding{
				Path:   legacyRoot,
				Detail: "legacy provisioned Git SSH key root",
				Repair: "move provisioned key directories into ~/.hazmat/secrets/git-ssh/provisioned/ or re-add them with the typed SSH inventory",
			})
		}
	case credentialCloudS3AccessKeyID:
		if cloud.AccessKey {
			findings = append(findings, credentialInventoryFinding{
				Path:   configFilePath,
				Detail: "legacy cloud access key field",
				Repair: "run `hazmat config cloud` or load the cloud config once to migrate the access key into ~/.hazmat/secrets/cloud/s3-access-key-id",
			})
		}
	case credentialCloudKopiaRecovery:
		if cloud.RecoveryKey || cloud.Password {
			field := "recovery key"
			if cloud.Password {
				field = "legacy password"
			}
			findings = append(findings, credentialInventoryFinding{
				Path:   configFilePath,
				Detail: "legacy cloud " + field + " field",
				Repair: "run `hazmat config cloud` or load the cloud config once to migrate the recovery key into ~/.hazmat/secrets/cloud/kopia-recovery-key",
			})
		}
	case credentialCloudS3SecretKey:
		if exists, err := credentialInventoryPathExists(cloudCredentialPath); err != nil {
			errors = append(errors, fmt.Sprintf("inspect legacy cloud credential file %s: %v", cloudCredentialPath, err))
		} else if exists {
			findings = append(findings, credentialInventoryFinding{
				Path:   cloudCredentialPath,
				Detail: "legacy cloud secret key file",
				Repair: "run `hazmat config cloud` or the next cloud backup/restore command to migrate the secret key into ~/.hazmat/secrets/cloud/s3-secret-key",
			})
		}
	case credentialProviderAnthropicAPIKey,
		credentialProviderOpenAIAPIKey,
		credentialProviderGeminiAPIKey,
		credentialHarnessClaudeCredentials,
		credentialHarnessClaudeState,
		credentialHarnessCodexAuth,
		credentialHarnessOpenCodeAuth,
		credentialHarnessGeminiOAuth,
		credentialHarnessGeminiAccounts,
		credentialHarnessGeminiKeychain,
		credentialGitSSHExternalIdentity:
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Path == findings[j].Path {
			return findings[i].Detail < findings[j].Detail
		}
		return findings[i].Path < findings[j].Path
	})
	return findings, errors
}

func inspectLegacyProviderExport(envVar string) (*credentialInventoryFinding, error) {
	if exists, err := credentialInventoryPathExists(agentZshrcPath); err != nil {
		return nil, fmt.Errorf("inspect legacy provider export %s in %s: %w", envVar, agentZshrcPath, err)
	} else if !exists {
		return nil, nil
	}
	data, err := credentialInventoryReadFile(agentZshrcPath)
	if err != nil {
		return nil, fmt.Errorf("read legacy provider export %s in %s: %w", envVar, agentZshrcPath, err)
	}
	prefix := "export " + envVar + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return &credentialInventoryFinding{
				Path:   agentZshrcPath,
				Detail: "legacy agent-home provider API-key export",
				Repair: "run `hazmat config agent` or launch the matching harness once to migrate the API key into ~/.hazmat/secrets/providers/ and remove the old export",
			}, nil
		}
	}
	return nil, nil
}

type legacyCloudCredentialConfig struct {
	AccessKey   bool
	RecoveryKey bool
	Password    bool
}

func inspectLegacyCloudCredentialConfig() (legacyCloudCredentialConfig, error) {
	data, err := credentialInventoryReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return legacyCloudCredentialConfig{}, nil
		}
		return legacyCloudCredentialConfig{}, err
	}
	var raw struct {
		Backup struct {
			Cloud *struct {
				AccessKey   string `yaml:"access_key"`
				RecoveryKey string `yaml:"recovery_key"`
				Password    string `yaml:"password"`
			} `yaml:"cloud"`
		} `yaml:"backup"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return legacyCloudCredentialConfig{}, err
	}
	if raw.Backup.Cloud == nil {
		return legacyCloudCredentialConfig{}, nil
	}
	return legacyCloudCredentialConfig{
		AccessKey:   strings.TrimSpace(raw.Backup.Cloud.AccessKey) != "",
		RecoveryKey: strings.TrimSpace(raw.Backup.Cloud.RecoveryKey) != "",
		Password:    strings.TrimSpace(raw.Backup.Cloud.Password) != "",
	}, nil
}

func credentialInventoryPathExistsOnDisk(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, nil
	}
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func legacyProvisionedSSHKeysRootDir() string {
	return filepath.Join(filepath.Dir(configFilePath), "ssh", "keys")
}
