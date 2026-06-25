package hazmat

import (
	"bytes"
	"fmt"
	"hazmat/credentials"
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

type optionalCredentialInventoryFinding struct {
	value   credentialInventoryFinding
	present bool
}

type credentialInventoryEntry struct {
	ID               credentials.ID
	DisplayName      string
	Kind             credentials.Kind
	Backend          credentials.StorageBackend
	Delivery         credentials.DeliveryMode
	Support          credentials.SupportStatus
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
	credentialInventoryPathExists      = credentialInventoryPathExistsOnDisk
	credentialInventoryReadFile        = os.ReadFile
	credentialInventoryAgentPathExists = agentPathExists
	credentialInventoryAgentReadFile   = agentReadFile
)

type credentialInventoryOptions struct {
	IncludeAgentState bool
}

func inspectCredentialInventory(home string) ([]credentialInventoryEntry, error) {
	return inspectCredentialInventoryWithOptions(home, credentialInventoryOptions{IncludeAgentState: true})
}

func inspectCredentialInventoryHostOnly(home string) ([]credentialInventoryEntry, error) {
	return inspectCredentialInventoryWithOptions(home, credentialInventoryOptions{})
}

func inspectCredentialInventoryWithOptions(home string, options credentialInventoryOptions) ([]credentialInventoryEntry, error) {
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

		if descriptor.Backend == credentials.StorageHostSecretStore {
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

		if options.IncludeAgentState {
			agentResidue, agentErrors := inspectDescriptorAgentResidue(descriptor)
			entry.AgentResidue = append(entry.AgentResidue, agentResidue...)
			entry.Errors = append(entry.Errors, agentErrors...)
		}
		legacy, errs := inspectDescriptorLegacyResidue(descriptor, legacyCloud, options)
		entry.LegacyResidue = append(entry.LegacyResidue, legacy...)
		entry.Errors = append(entry.Errors, errs...)
		if legacyCloudErr != nil && descriptor.Kind == credentials.KindCloudBackup {
			entry.Errors = append(entry.Errors, fmt.Sprintf("inspect legacy cloud config %s: %v", configFilePath, legacyCloudErr))
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func summarizeCredentialInventory(entries []credentialInventoryEntry) credentialInventorySummary {
	summary := credentialInventorySummary{Total: len(entries)}
	for _, entry := range entries {
		if entry.Support == credentials.SupportManaged && entry.Backend == credentials.StorageHostSecretStore {
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
	case entry.Support == credentials.SupportAdapterRequired:
		return credentialInventoryAdapterRequired
	case entry.Support == credentials.SupportExternal:
		return credentialInventoryExternal
	case entry.Backend == credentials.StorageHostSecretStore && entry.HostStorePresent:
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
	if entry.Backend == credentials.StorageHostSecretStore {
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

func inspectDescriptorAgentResidue(descriptor credentials.Descriptor) ([]credentialInventoryFinding, []string) {
	switch descriptor.Delivery {
	case credentials.DeliveryMaterializedFile:
		if descriptor.AgentPath == "" {
			return nil, nil
		}
		if descriptor.ID == credentials.HarnessClaudeState {
			return inspectClaudeStateAgentResidue(descriptor.AgentPath)
		}
		if exists, err := credentialInventoryAgentPathExists(descriptor.AgentPath); err != nil {
			return nil, []string{fmt.Sprintf("inspect agent credential path %s: %v", descriptor.AgentPath, err)}
		} else if exists {
			return []credentialInventoryFinding{{
				Path:   descriptor.AgentPath,
				Detail: "stale agent-home materialized credential",
				Repair: "Hazmat can harvest this into the host-owned secret store during credential repair; remove the stale file only after verifying the host store",
			}}, nil
		}
	case credentials.DeliveryBrokeredHelper:
		if descriptor.ID == credentials.GitHTTPSAgentStore {
			if exists, err := credentialInventoryAgentPathExists(gitHTTPSAgentCredentialsPath); err != nil {
				return nil, []string{fmt.Sprintf("inspect Git HTTPS credential path %s: %v", gitHTTPSAgentCredentialsPath, err)}
			} else if exists {
				return []credentialInventoryFinding{{
					Path:   gitHTTPSAgentCredentialsPath,
					Detail: "legacy agent-home Git HTTPS credential store",
					Repair: "Hazmat can migrate this into ~/.hazmat/secrets/git-https/credentials during credential repair; rotate and remove old PATs if migration is not desired",
				}}, nil
			}
		}
	case credentials.DeliveryNone, credentials.DeliveryEnv, credentials.DeliveryExternalReference:
	}
	return nil, nil
}

func inspectClaudeStateAgentResidue(path string) ([]credentialInventoryFinding, []string) {
	if exists, err := credentialInventoryAgentPathExists(path); err != nil {
		return nil, []string{fmt.Sprintf("inspect agent credential path %s: %v", path, err)}
	} else if !exists {
		return nil, nil
	}

	raw, err := credentialInventoryAgentReadFile(path)
	if err != nil {
		return nil, []string{fmt.Sprintf("inspect Claude state keys %s: %v", path, err)}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	payload, err := selectClaudeAuthKeys(raw)
	if err != nil {
		return nil, []string{fmt.Sprintf("inspect Claude state keys %s: %v", path, err)}
	}
	if len(payload) == 0 {
		return nil, nil
	}

	return []credentialInventoryFinding{{
		Path:   path,
		Detail: "stale agent-home Claude portable auth state",
		Repair: "Hazmat can harvest these keys into the host-owned secret store during credential repair; non-auth Claude settings may remain in the agent state file",
	}}, nil
}

func inspectDescriptorLegacyResidue(descriptor credentials.Descriptor, cloud legacyCloudCredentialConfig, options credentialInventoryOptions) ([]credentialInventoryFinding, []string) {
	var findings []credentialInventoryFinding
	var errors []string

	if options.IncludeAgentState && descriptor.Kind == credentials.KindProviderAPIKey && descriptor.EnvVar != "" {
		finding, err := inspectLegacyProviderExport(descriptor.EnvVar)
		if err != nil {
			errors = append(errors, err.Error())
		} else if finding.present {
			findings = append(findings, finding.value)
		}
	}

	switch descriptor.ID {
	case credentials.GitHTTPSAgentStore:
		if !options.IncludeAgentState {
			break
		}
		if hasHelper, err := inspectAgentLegacyGitHTTPSCredentialHelper(gitHTTPSAgentGitConfigPath); err != nil {
			errors = append(errors, fmt.Sprintf("inspect legacy Git HTTPS helper %s: %v", gitHTTPSAgentGitConfigPath, err))
		} else if hasHelper {
			findings = append(findings, credentialInventoryFinding{
				Path:   gitHTTPSAgentGitConfigPath,
				Detail: "legacy persistent Git HTTPS credential helper",
				Repair: "Hazmat can remove the persistent helper during credential repair; native sessions inject a brokered helper only while running",
			})
		}
	case credentials.GitSSHProvisionedIdentity:
		legacyRoot := legacyProvisionedSSHKeysRootDir()
		if exists, err := credentialInventoryPathExists(legacyRoot); err != nil {
			errors = append(errors, fmt.Sprintf("inspect legacy Git SSH key root %s: %v", legacyRoot, err))
		} else if exists {
			findings = append(findings, credentialInventoryFinding{
				Path:   legacyRoot,
				Detail: "legacy provisioned Git SSH key root",
				Repair: "Move provisioned key directories into ~/.hazmat/secrets/git-ssh/provisioned/ through the typed SSH inventory before removing the legacy root",
			})
		}
	case credentials.CloudS3AccessKeyID:
		if cloud.AccessKey {
			findings = append(findings, credentialInventoryFinding{
				Path:   configFilePath,
				Detail: "legacy cloud access key field",
				Repair: "Hazmat can migrate this into ~/.hazmat/secrets/cloud/s3-access-key-id during credential repair so the legacy field is no longer authoritative",
			})
		}
	case credentials.CloudKopiaRecovery:
		if cloud.RecoveryKey || cloud.Password {
			field := "recovery key"
			if cloud.Password {
				field = "legacy password"
			}
			findings = append(findings, credentialInventoryFinding{
				Path:   configFilePath,
				Detail: "legacy cloud " + field + " field",
				Repair: "Hazmat can migrate this into ~/.hazmat/secrets/cloud/kopia-recovery-key during credential repair so the legacy field is no longer authoritative",
			})
		}
	case credentials.CloudS3SecretKey:
		if exists, err := credentialInventoryPathExists(cloudCredentialPath); err != nil {
			errors = append(errors, fmt.Sprintf("inspect legacy cloud credential file %s: %v", cloudCredentialPath, err))
		} else if exists {
			findings = append(findings, credentialInventoryFinding{
				Path:   cloudCredentialPath,
				Detail: "legacy cloud secret key file",
				Repair: "Hazmat can migrate this into ~/.hazmat/secrets/cloud/s3-secret-key during credential repair so the legacy file is no longer authoritative",
			})
		}
	case credentials.ProviderAnthropicAPIKey,
		credentials.ProviderOpenAIAPIKey,
		credentials.ProviderGeminiAPIKey,
		credentials.ProviderAntigravityAPIKey,
		credentials.ProviderOpenRouterAPIKey,
		credentials.GitHubAPIToken,
		credentials.HarnessClaudeCredentials,
		credentials.HarnessClaudeState,
		credentials.HarnessCodexAuth,
		credentials.HarnessOpenCodeAuth,
		credentials.HarnessClaudeKeychain,
		credentials.HarnessAntigravityKeychain,
		credentials.GitSSHExternalIdentity:
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Path == findings[j].Path {
			return findings[i].Detail < findings[j].Detail
		}
		return findings[i].Path < findings[j].Path
	})
	return findings, errors
}

func inspectLegacyProviderExport(envVar string) (optionalCredentialInventoryFinding, error) {
	if exists, err := credentialInventoryAgentPathExists(agentZshrcPath); err != nil {
		return optionalCredentialInventoryFinding{}, fmt.Errorf("inspect legacy provider export %s in %s: %w", envVar, agentZshrcPath, err)
	} else if !exists {
		return optionalCredentialInventoryFinding{}, nil
	}
	data, err := credentialInventoryAgentReadFile(agentZshrcPath)
	if err != nil {
		return optionalCredentialInventoryFinding{}, fmt.Errorf("read legacy provider export %s in %s: %w", envVar, agentZshrcPath, err)
	}
	prefix := "export " + envVar + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return optionalCredentialInventoryFinding{
				value: credentialInventoryFinding{
					Path:   agentZshrcPath,
					Detail: "legacy agent-home provider API-key export",
					Repair: "Hazmat can migrate this into ~/.hazmat/secrets/providers/ during credential repair; remove the old export only after verifying the host store",
				},
				present: true,
			}, nil
		}
	}
	return optionalCredentialInventoryFinding{}, nil
}

func inspectAgentLegacyGitHTTPSCredentialHelper(path string) (bool, error) {
	exists, err := credentialInventoryAgentPathExists(path)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	data, err := credentialInventoryAgentReadFile(path)
	if err != nil {
		return false, err
	}
	return gitConfigDataHasLegacyGitHTTPSCredentialHelper(data), nil
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
