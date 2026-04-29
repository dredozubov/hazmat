package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	credentialMigrationHarnesses = []HarnessID{
		HarnessClaude,
		HarnessCodex,
		HarnessOpenCode,
		HarnessGemini,
	}
	credentialMigrationHarnessAuthArtifactsForHome = harnessAuthArtifactsForHome
)

type migrateCredentialsOptions struct {
	DryRun bool
	Writer io.Writer
	Home   string
}

type migrateCredentialEventKind string

const (
	migrateCredentialEventAction  migrateCredentialEventKind = "ok"
	migrateCredentialEventInfo    migrateCredentialEventKind = "info"
	migrateCredentialEventWarning migrateCredentialEventKind = "warn"
	migrateCredentialEventError   migrateCredentialEventKind = "error"
)

type migrateCredentialEvent struct {
	Kind      migrateCredentialEventKind
	Component string
	Message   string
}

type migrateCredentialsReport struct {
	Events []migrateCredentialEvent
	Errors []error
}

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run one-shot user data migrations and repairs",
		Long: `Run explicit, idempotent migrations for user-owned Hazmat state.

These commands are separate from init-version migrations: they repair durable
user data left by older Hazmat releases or interrupted sessions.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newMigrateCredentialsCmd())
	return cmd
}

func newMigrateCredentialsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "credentials",
		Short: "Move legacy credential residue into the host-owned secret store",
		Long: `Move legacy credential residue into Hazmat's host-owned secret store.

The command is safe to rerun. It repairs provider API-key exports, file-backed
harness auth residue, Git HTTPS credential/helper residue, cloud credential
fields/files, and legacy provisioned Git SSH inventory roots without printing
secret values.

Examples:
  hazmat migrate credentials
  hazmat migrate credentials --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMigrateCredentials(migrateCredentialsOptions{
				DryRun: flagDryRun,
				Writer: os.Stdout,
			})
		},
	}
}

func runMigrateCredentials(opts migrateCredentialsOptions) error {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	home := opts.Home
	if strings.TrimSpace(home) == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("determine home directory for credential migration: %w", err)
		}
	}

	report := &migrateCredentialsReport{}
	migrateCredentialProviderAPIKeys(opts.DryRun, report)
	migrateCredentialHarnessAuth(home, opts.DryRun, report)
	migrateCredentialGitHTTPS(home, opts.DryRun, report)
	migrateCredentialCloud(opts.DryRun, report)
	migrateCredentialProvisionedGitSSH(opts.DryRun, report)
	reportCredentialMigrationBoundaries(report)

	renderMigrateCredentialsReport(w, opts.DryRun, report)
	if len(report.Errors) > 0 {
		return fmt.Errorf("credential migration completed with %d error(s): %w",
			len(report.Errors), errors.Join(report.Errors...))
	}
	return nil
}

func (r *migrateCredentialsReport) action(component, message string) {
	r.Events = append(r.Events, migrateCredentialEvent{
		Kind:      migrateCredentialEventAction,
		Component: component,
		Message:   message,
	})
}

func (r *migrateCredentialsReport) info(component, message string) {
	r.Events = append(r.Events, migrateCredentialEvent{
		Kind:      migrateCredentialEventInfo,
		Component: component,
		Message:   message,
	})
}

func (r *migrateCredentialsReport) warn(component, message string) {
	r.Events = append(r.Events, migrateCredentialEvent{
		Kind:      migrateCredentialEventWarning,
		Component: component,
		Message:   message,
	})
}

func (r *migrateCredentialsReport) err(component string, err error) {
	if err == nil {
		return
	}
	r.Errors = append(r.Errors, fmt.Errorf("%s: %w", component, err))
	r.Events = append(r.Events, migrateCredentialEvent{
		Kind:      migrateCredentialEventError,
		Component: component,
		Message:   err.Error(),
	})
}

func renderMigrateCredentialsReport(w io.Writer, dryRun bool, report *migrateCredentialsReport) {
	title := "Credential migration repair"
	if dryRun {
		title += " (dry run)"
	}
	fmt.Fprintln(w, title)

	actionCount := 0
	for _, event := range report.Events {
		if event.Kind == migrateCredentialEventAction {
			actionCount++
		}
		fmt.Fprintf(w, "  %s [%s] %s\n", event.Kind, event.Component, event.Message)
	}
	if actionCount == 0 && len(report.Errors) == 0 {
		fmt.Fprintln(w, "  ok [credentials] no legacy credential residue found")
	}
}

func migrateCredentialProviderAPIKeys(dryRun bool, report *migrateCredentialsReport) {
	for _, spec := range harnessAPIKeyPrompts {
		legacyLine := readZshrcEnvLine(agentZshrcPath, spec.EnvVar)
		if legacyLine == "" {
			continue
		}

		storePath, err := providerSecretStorePath(spec.EnvVar)
		if err != nil {
			report.err("provider", err)
			continue
		}
		storedValue, err := readHostStoredAPIKey(spec)
		if err != nil {
			report.err("provider", err)
			continue
		}

		legacyValue, parsed := parseExportedEnvLineValue(legacyLine, spec.EnvVar)
		legacyValue = strings.TrimSpace(legacyValue)
		if !parsed || legacyValue == "" {
			report.warn("provider", fmt.Sprintf("legacy %s export exists in %s but could not be parsed; leaving it in place", spec.EnvVar, agentZshrcPath))
			continue
		}

		switch {
		case storedValue == "":
			if dryRun {
				report.action("provider", fmt.Sprintf("would migrate legacy %s from %s into ~/.hazmat/secrets/providers/", spec.EnvVar, agentZshrcPath))
				continue
			}
			if err := storeHostAPIKey(spec, legacyValue); err != nil {
				report.err("provider", fmt.Errorf("migrate legacy %s into host secret store: %w", spec.EnvVar, err))
				continue
			}
			if err := removeLegacyAPIKeyExports([]string{spec.EnvVar}); err != nil {
				report.err("provider", fmt.Errorf("remove legacy %s export from %s: %w", spec.EnvVar, agentZshrcPath, err))
				continue
			}
			report.action("provider", fmt.Sprintf("migrated legacy %s from %s into ~/.hazmat/secrets/providers/", spec.EnvVar, agentZshrcPath))
		case strings.TrimSpace(storedValue) == legacyValue:
			if dryRun {
				report.action("provider", fmt.Sprintf("would remove duplicate legacy %s export from %s", spec.EnvVar, agentZshrcPath))
				continue
			}
			if err := removeLegacyAPIKeyExports([]string{spec.EnvVar}); err != nil {
				report.err("provider", fmt.Errorf("remove duplicate legacy %s export from %s: %w", spec.EnvVar, agentZshrcPath, err))
				continue
			}
			report.action("provider", fmt.Sprintf("removed duplicate legacy %s export from %s", spec.EnvVar, agentZshrcPath))
		default:
			if dryRun {
				report.action("provider", fmt.Sprintf("would archive divergent legacy %s from %s under %s.conflicts/ and remove the export", spec.EnvVar, agentZshrcPath, storePath))
				continue
			}
			conflictPath, err := preserveSecretStoreConflict(storePath, []byte(legacyValue+"\n"))
			if err != nil {
				report.err("provider", fmt.Errorf("archive divergent legacy %s from %s: %w", spec.EnvVar, agentZshrcPath, err))
				continue
			}
			if err := removeLegacyAPIKeyExports([]string{spec.EnvVar}); err != nil {
				report.err("provider", fmt.Errorf("remove divergent legacy %s export from %s after archive: %w", spec.EnvVar, agentZshrcPath, err))
				continue
			}
			report.action("provider", fmt.Sprintf("archived divergent legacy %s at %s and removed the export from %s", spec.EnvVar, conflictPath, agentZshrcPath))
		}
	}
}

func migrateCredentialHarnessAuth(home string, dryRun bool, report *migrateCredentialsReport) {
	for _, harness := range credentialMigrationHarnesses {
		artifacts := credentialMigrationHarnessAuthArtifactsForHome(harness, home)
		if dryRun {
			notes, errs := previewHarnessAuthArtifactMigration(artifacts)
			for _, err := range errs {
				report.err("harness-auth", err)
			}
			for _, note := range notes {
				report.action("harness-auth", note)
			}
			continue
		}

		var notes []string
		if err := migrateHarnessAuthArtifacts(artifacts, func(note string) {
			notes = append(notes, note)
		}); err != nil {
			report.err("harness-auth", err)
			continue
		}
		for _, note := range notes {
			report.action("harness-auth", note)
		}
	}
}

func previewHarnessAuthArtifactMigration(artifacts []harnessAuthArtifact) ([]string, []error) {
	var notes []string
	var errs []error
	for _, artifact := range artifacts {
		stored, storedExists, err := artifact.ReadStore(artifact.StorePath)
		if err != nil {
			errs = append(errs, fmt.Errorf("read host-owned %s: %w", artifact.Name, err))
			continue
		}
		legacy, legacyExists, err := artifact.ReadAgent(artifact.AgentPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("read legacy %s from %s: %w", artifact.Name, artifact.AgentPath, err))
			continue
		}

		switch {
		case !storedExists && legacyExists:
			notes = append(notes, fmt.Sprintf("would migrate legacy %s from %s into ~/.hazmat/secrets", artifact.Name, artifact.AgentPath))
		case storedExists && legacyExists && artifact.Equal(stored, legacy):
			notes = append(notes, fmt.Sprintf("would remove duplicate legacy %s from %s", artifact.Name, artifact.AgentPath))
		case storedExists && legacyExists:
			notes = append(notes, fmt.Sprintf("would archive previous host-owned %s, promote the divergent legacy copy from %s, and remove agent residue", artifact.Name, artifact.AgentPath))
		}
	}
	return notes, errs
}

func migrateCredentialGitHTTPS(home string, dryRun bool, report *migrateCredentialsReport) {
	storePath := gitHTTPSCredentialStorePathForHome(home)
	legacy, legacyExists, err := readAgentSecretFile(gitHTTPSAgentCredentialsPath)
	if err != nil {
		report.err("git-https", fmt.Errorf("read legacy Git HTTPS credential store %s: %w", gitHTTPSAgentCredentialsPath, err))
	} else if legacyExists {
		if dryRun {
			if len(bytes.TrimSpace(legacy)) == 0 {
				report.action("git-https", fmt.Sprintf("would remove empty legacy Git HTTPS credential store %s", gitHTTPSAgentCredentialsPath))
			} else {
				report.action("git-https", fmt.Sprintf("would merge legacy Git HTTPS credential store %s into ~/.hazmat/secrets/git-https/credentials", gitHTTPSAgentCredentialsPath))
			}
		} else {
			migrated, err := migrateLegacyGitHTTPSCredentials(storePath)
			if err != nil {
				report.err("git-https", fmt.Errorf("migrate legacy Git HTTPS credential store %s: %w", gitHTTPSAgentCredentialsPath, err))
			} else if migrated {
				report.action("git-https", fmt.Sprintf("migrated legacy Git HTTPS credential store %s into ~/.hazmat/secrets/git-https/credentials", gitHTTPSAgentCredentialsPath))
			}
		}
	}

	if !hasLegacyGitHTTPSCredentialHelper(gitHTTPSAgentGitConfigPath) {
		return
	}
	if dryRun {
		report.action("git-https", fmt.Sprintf("would remove legacy Git HTTPS credential helper from %s", gitHTTPSAgentGitConfigPath))
		return
	}
	if err := updateAgentFile(
		gitHTTPSAgentGitConfigPath,
		func(content string) string {
			cfg := parseINI(content)
			cfg = removeINIValues(cfg, "credential", "helper", isLegacyGitHTTPSCredentialHelperValue)
			return renderINI(cfg)
		},
		0o644,
	); err != nil {
		report.err("git-https", fmt.Errorf("remove legacy Git HTTPS credential helper from %s: %w", gitHTTPSAgentGitConfigPath, err))
		return
	}
	report.action("git-https", fmt.Sprintf("removed legacy Git HTTPS credential helper from %s", gitHTTPSAgentGitConfigPath))
}

func migrateCredentialCloud(dryRun bool, report *migrateCredentialsReport) {
	if err := migrateLegacyCloudConfigSecrets(dryRun, report); err != nil {
		report.err("cloud", err)
	}
	if err := migrateLegacyCloudSecretFile(dryRun, report); err != nil {
		report.err("cloud", err)
	}
}

func migrateLegacyCloudConfigSecrets(dryRun bool, report *migrateCredentialsReport) error {
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy cloud config %s: %w", configFilePath, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse legacy cloud config %s: %w", configFilePath, err)
	}
	cloud := cloudConfigYAMLNode(&doc)
	if cloud == nil {
		return nil
	}

	accessKey := strings.TrimSpace(yamlMappingScalar(cloud, "access_key"))
	recoveryKey := strings.TrimSpace(yamlMappingScalar(cloud, "recovery_key"))
	password := strings.TrimSpace(yamlMappingScalar(cloud, "password"))
	if accessKey == "" && recoveryKey == "" && password == "" {
		return nil
	}

	if dryRun {
		if accessKey != "" {
			report.action("cloud", "would migrate legacy backup.cloud.access_key into ~/.hazmat/secrets/cloud/s3-access-key-id")
		}
		if recoveryKey != "" {
			report.action("cloud", "would migrate legacy backup.cloud.recovery_key into ~/.hazmat/secrets/cloud/kopia-recovery-key")
			if password != "" {
				report.action("cloud", "would scrub legacy backup.cloud.password; recovery_key remains authoritative")
			}
		} else if password != "" {
			report.action("cloud", "would migrate legacy backup.cloud.password into ~/.hazmat/secrets/cloud/kopia-recovery-key")
		}
		return nil
	}

	if accessKey != "" {
		if err := saveCloudStoredCredential(credentialCloudS3AccessKeyID, accessKey); err != nil {
			return fmt.Errorf("migrate legacy backup.cloud.access_key: %w", err)
		}
		report.action("cloud", "migrated legacy backup.cloud.access_key into ~/.hazmat/secrets/cloud/s3-access-key-id")
	}
	if recoveryKey != "" {
		if err := saveCloudStoredCredential(credentialCloudKopiaRecovery, recoveryKey); err != nil {
			return fmt.Errorf("migrate legacy backup.cloud.recovery_key: %w", err)
		}
		report.action("cloud", "migrated legacy backup.cloud.recovery_key into ~/.hazmat/secrets/cloud/kopia-recovery-key")
	} else if password != "" {
		if err := saveCloudStoredCredential(credentialCloudKopiaRecovery, password); err != nil {
			return fmt.Errorf("migrate legacy backup.cloud.password: %w", err)
		}
		report.action("cloud", "migrated legacy backup.cloud.password into ~/.hazmat/secrets/cloud/kopia-recovery-key")
	}
	removeYAMLMappingKeys(cloud, "access_key", "recovery_key", "password")
	encoded, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal scrubbed cloud config %s: %w", configFilePath, err)
	}
	if err := os.WriteFile(configFilePath, encoded, 0o600); err != nil {
		return fmt.Errorf("write scrubbed cloud config %s: %w", configFilePath, err)
	}
	return nil
}

func migrateLegacyCloudSecretFile(dryRun bool, report *migrateCredentialsReport) error {
	raw, err := os.ReadFile(cloudCredentialPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy cloud credential file %s: %w", cloudCredentialPath, err)
	}
	secret := strings.TrimSpace(string(raw))
	if dryRun {
		if secret == "" {
			report.action("cloud", fmt.Sprintf("would remove empty legacy cloud credential file %s", cloudCredentialPath))
		} else {
			report.action("cloud", fmt.Sprintf("would migrate legacy cloud credential file %s into ~/.hazmat/secrets/cloud/s3-secret-key", cloudCredentialPath))
		}
		return nil
	}
	if secret == "" {
		if err := os.Remove(cloudCredentialPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty legacy cloud credential file %s: %w", cloudCredentialPath, err)
		}
		report.action("cloud", fmt.Sprintf("removed empty legacy cloud credential file %s", cloudCredentialPath))
		return nil
	}

	storePath, err := cloudCredentialStorePath(credentialCloudS3SecretKey)
	if err != nil {
		return err
	}
	hostRaw, hostExists, err := readHostStoredSecretFile(storePath)
	if err != nil {
		return fmt.Errorf("read host cloud secret key store: %w", err)
	}
	switch {
	case !hostExists:
		if err := writeHostStoredSecretFile(storePath, []byte(secret+"\n")); err != nil {
			return fmt.Errorf("write cloud secret key to host secret store: %w", err)
		}
		report.action("cloud", fmt.Sprintf("migrated legacy cloud credential file %s into ~/.hazmat/secrets/cloud/s3-secret-key", cloudCredentialPath))
	case bytes.Equal(bytes.TrimSpace(hostRaw), []byte(secret)):
		report.action("cloud", fmt.Sprintf("removed duplicate legacy cloud credential file %s", cloudCredentialPath))
	default:
		conflictPath, err := preserveSecretStoreConflict(storePath, []byte(secret+"\n"))
		if err != nil {
			return fmt.Errorf("archive divergent legacy cloud credential file %s: %w", cloudCredentialPath, err)
		}
		report.action("cloud", fmt.Sprintf("archived divergent legacy cloud credential file at %s", conflictPath))
	}
	if err := os.Remove(cloudCredentialPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy cloud credential file %s: %w", cloudCredentialPath, err)
	}
	return nil
}

func cloudConfigYAMLNode(doc *yaml.Node) *yaml.Node {
	root := yamlDocumentRoot(doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	backup := yamlMappingNode(root, "backup")
	if backup == nil || backup.Kind != yaml.MappingNode {
		return nil
	}
	cloud := yamlMappingNode(backup, "cloud")
	if cloud == nil || cloud.Kind != yaml.MappingNode {
		return nil
	}
	return cloud
}

func yamlDocumentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	return doc
}

func yamlMappingNode(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func yamlMappingScalar(mapping *yaml.Node, key string) string {
	node := yamlMappingNode(mapping, key)
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func removeYAMLMappingKeys(mapping *yaml.Node, keys ...string) {
	toRemove := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		toRemove[key] = struct{}{}
	}
	filtered := mapping.Content[:0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		valueNode := mapping.Content[i+1]
		if _, ok := toRemove[keyNode.Value]; ok {
			continue
		}
		filtered = append(filtered, keyNode, valueNode)
	}
	mapping.Content = filtered
}

func migrateCredentialProvisionedGitSSH(dryRun bool, report *migrateCredentialsReport) {
	legacyRoot := legacyProvisionedSSHKeysRootDir()
	info, err := os.Stat(legacyRoot)
	if err != nil {
		if !os.IsNotExist(err) {
			report.err("git-ssh", fmt.Errorf("inspect legacy provisioned Git SSH key root %s: %w", legacyRoot, err))
		}
		return
	}
	if !info.IsDir() {
		report.warn("git-ssh", fmt.Sprintf("legacy provisioned Git SSH key root %s exists but is not a directory; leaving it in place", legacyRoot))
		return
	}

	newRoot := provisionedSSHKeysRootDir()
	if dryRun {
		report.action("git-ssh", fmt.Sprintf("would migrate legacy provisioned Git SSH key root %s into %s", legacyRoot, newRoot))
		return
	}

	if _, err := os.Stat(newRoot); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(newRoot), 0o700); err != nil {
			report.err("git-ssh", fmt.Errorf("create provisioned Git SSH secret-store root parent: %w", err))
			return
		}
		if err := os.Rename(legacyRoot, newRoot); err != nil {
			report.err("git-ssh", fmt.Errorf("move legacy provisioned Git SSH key root %s to %s: %w", legacyRoot, newRoot, err))
			return
		}
		report.action("git-ssh", fmt.Sprintf("migrated legacy provisioned Git SSH key root %s into %s", legacyRoot, newRoot))
		removeEmptyLegacySSHParents(legacyRoot)
		return
	} else if err != nil {
		report.err("git-ssh", fmt.Errorf("inspect provisioned Git SSH secret-store root %s: %w", newRoot, err))
		return
	}

	if err := os.MkdirAll(newRoot, 0o700); err != nil {
		report.err("git-ssh", fmt.Errorf("create provisioned Git SSH secret-store root %s: %w", newRoot, err))
		return
	}
	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		report.err("git-ssh", fmt.Errorf("read legacy provisioned Git SSH key root %s: %w", legacyRoot, err))
		return
	}
	for _, entry := range entries {
		src := filepath.Join(legacyRoot, entry.Name())
		dst := filepath.Join(newRoot, entry.Name())
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			if entry.IsDir() {
				if err := os.Rename(src, dst); err != nil {
					report.err("git-ssh", fmt.Errorf("move legacy provisioned Git SSH key %s into %s: %w", src, dst, err))
					continue
				}
				report.action("git-ssh", fmt.Sprintf("migrated legacy provisioned Git SSH key %s into %s", entry.Name(), newRoot))
				continue
			}
		} else if err != nil {
			report.err("git-ssh", fmt.Errorf("inspect provisioned Git SSH destination %s: %w", dst, err))
			continue
		}

		conflictPath := nextDirectoryConflictPath(filepath.Join(filepath.Dir(newRoot), "provisioned.conflicts", entry.Name()))
		if err := os.MkdirAll(filepath.Dir(conflictPath), 0o700); err != nil {
			report.err("git-ssh", fmt.Errorf("create provisioned Git SSH conflict parent for %s: %w", src, err))
			continue
		}
		if err := os.Rename(src, conflictPath); err != nil {
			report.err("git-ssh", fmt.Errorf("archive legacy provisioned Git SSH key %s at %s: %w", src, conflictPath, err))
			continue
		}
		report.action("git-ssh", fmt.Sprintf("archived legacy provisioned Git SSH key %s at %s", entry.Name(), conflictPath))
	}
	if err := os.Remove(legacyRoot); err != nil && !os.IsNotExist(err) {
		report.warn("git-ssh", fmt.Sprintf("legacy provisioned Git SSH key root %s is not empty after migration; leaving remaining entries in place", legacyRoot))
		return
	}
	removeEmptyLegacySSHParents(legacyRoot)
}

func removeEmptyLegacySSHParents(legacyRoot string) {
	_ = os.Remove(filepath.Dir(legacyRoot))
}

func nextDirectoryConflictPath(root string) string {
	stamp := harnessAuthConflictNow().UTC().Format("20060102T150405.000000000Z")
	for i := 0; ; i++ {
		name := stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", stamp, i)
		}
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err != nil {
			return path
		}
	}
}

func preserveSecretStoreConflict(storePath string, raw []byte) (string, error) {
	path := nextHarnessAuthConflictPath(storePath)
	if err := writeHostStoredSecretFile(path, raw); err != nil {
		return "", err
	}
	return path, nil
}

func reportCredentialMigrationBoundaries(report *migrateCredentialsReport) {
	summary := summarizeCredentialRegistry(builtinCredentialDescriptors())
	if len(summary.ExternalBoundaries) > 0 {
		report.info("boundaries", fmt.Sprintf("external credentials are referenced, not imported: %s", strings.Join(summary.ExternalBoundaries, ", ")))
	}
	if len(summary.AdapterRequired) > 0 {
		report.info("boundaries", fmt.Sprintf("adapter-required credentials are left untouched: %s", strings.Join(summary.AdapterRequired, ", ")))
	}
}
