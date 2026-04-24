package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type suggestedIntegrationAction string

const (
	suggestedIntegrationActionUseNow suggestedIntegrationAction = "use-now"
	suggestedIntegrationActionAlways suggestedIntegrationAction = "always"
	suggestedIntegrationActionNotNow suggestedIntegrationAction = "not-now"
)

const suggestedIntegrationActionPrompt = "How should Hazmat use this selection? [1-3, Enter for default]:"

type suggestedIntegrationPromptItem struct {
	Name        string
	Description string
}

type suggestedIntegrationPromptResult struct {
	Action   suggestedIntegrationAction
	Selected []string
}

type launchIntegrationResolution struct {
	Integrations []IntegrationSpec
}

var promptSuggestedLaunchIntegrations = defaultPromptSuggestedLaunchIntegrations

func prepareLaunchSession(commandName string, opts harnessSessionOpts, supportsSandbox bool) (preparedSession, error) {
	progress := newSessionPreparationProgress(os.Stderr)
	progress.Step("resolving launch context")
	projectDir, err := resolveDir(opts.project, true)
	if err != nil {
		return preparedSession{}, err
	}

	progress.Step("checking suggested integrations")
	resolvedIntegrations, err := resolveLaunchIntegrations(projectDir, opts.integrations)
	if err != nil {
		return preparedSession{}, err
	}
	opts.resolvedIntegrations = resolvedIntegrations.Integrations
	opts.integrationsResolved = true

	prepared, err := resolvePreparedSessionWithProgress(commandName, opts, supportsSandbox, progress)
	if err != nil {
		return preparedSession{}, err
	}
	progress.Step("finalizing repo setup")
	prepared, err = finalizePreparedRepoSetup(prepared, true, true)
	if err != nil {
		return preparedSession{}, err
	}
	progress.Done()
	return prepared, nil
}

func resolveLaunchIntegrations(projectDir string, integrationFlags []string) (launchIntegrationResolution, error) {
	baseFlags := dedupeStrings(integrationFlags)
	integrations, err := resolveActiveIntegrationsForSession(baseFlags, projectDir)
	if err != nil {
		return launchIntegrationResolution{}, err
	}
	return launchIntegrationResolution{Integrations: integrations}, nil
}

func resolveLaunchIntegrationFlags(projectDir string, integrationFlags []string) ([]string, error) {
	baseFlags := dedupeStrings(integrationFlags)

	integrations, err := resolveActiveIntegrationsForSession(baseFlags, projectDir)
	if err != nil {
		return nil, err
	}
	if len(integrations) == 0 {
		return baseFlags, nil
	}
	flags := make([]string, 0, len(integrations))
	for _, spec := range integrations {
		flags = append(flags, spec.Meta.Name)
	}
	sort.Strings(flags)
	return flags, nil
}

func mergeSelectedLaunchIntegrations(existing []IntegrationSpec, selected []string) ([]IntegrationSpec, error) {
	if len(selected) == 0 {
		return existing, nil
	}

	merged := make([]IntegrationSpec, 0, len(existing)+len(selected))
	seen := make(map[string]struct{}, len(existing)+len(selected))
	for _, spec := range existing {
		merged = append(merged, spec)
		seen[spec.Meta.Name] = struct{}{}
	}
	for _, name := range selected {
		if _, ok := seen[name]; ok {
			continue
		}
		spec, err := loadIntegrationSpecByName(name)
		if err != nil {
			return nil, err
		}
		merged = append(merged, spec)
		seen[name] = struct{}{}
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Meta.Name < merged[j].Meta.Name
	})
	return merged, nil
}

func shouldPromptSuggestedIntegrations() bool {
	if flagDryRun {
		return false
	}
	return flagYesAll || uiIsTerminal()
}

func suggestedIntegrationsForProject(projectDir string, activeNames map[string]struct{}) []string {
	suggestions := suggestIntegrations(projectDir, activeNames)
	suggestions = append(suggestions, unapprovedRepoRecommendedIntegrations(projectDir, activeNames)...)
	return filterRejectedSuggestedIntegrations(projectDir, dedupeAndSortStrings(suggestions))
}

func unapprovedRepoRecommendedIntegrations(projectDir string, activeNames map[string]struct{}) []string {
	names, fileHash, err := loadRepoRecommendations(projectDir)
	if err != nil || len(names) == 0 || isApproved(projectDir, fileHash) {
		return nil
	}
	var suggestions []string
	for _, name := range names {
		if _, active := activeNames[name]; active {
			continue
		}
		suggestions = append(suggestions, name)
	}
	return dedupeAndSortStrings(suggestions)
}

func filterRejectedSuggestedIntegrations(projectDir string, suggestions []string) []string {
	if len(suggestions) == 0 {
		return nil
	}

	cfg, err := loadConfig()
	if err != nil {
		return append([]string(nil), suggestions...)
	}

	rejected := stringSet(cfg.ProjectRejectedIntegrations(projectDir))
	if len(rejected) == 0 {
		return append([]string(nil), suggestions...)
	}

	filtered := make([]string, 0, len(suggestions))
	for _, name := range suggestions {
		if _, blocked := rejected[name]; blocked {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}

func buildSuggestedIntegrationPromptItems(names []string) []suggestedIntegrationPromptItem {
	items := make([]suggestedIntegrationPromptItem, 0, len(names))
	for _, name := range names {
		description := "Suggested by project files."
		if spec, err := loadBuiltinIntegrationSpec(name); err == nil {
			if spec.Meta.Description != "" {
				description = spec.Meta.Description
			}
		}
		items = append(items, suggestedIntegrationPromptItem{
			Name:        name,
			Description: description,
		})
	}
	return items
}

func defaultPromptSuggestedLaunchIntegrations(projectDir string, items []suggestedIntegrationPromptItem) (suggestedIntegrationPromptResult, error) {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}

	fmt.Println()
	fmt.Fprintf(os.Stderr, "hazmat: suggested integrations detected for %s\n", projectDir)

	choices := make([]UIChoice, 0, len(items))
	defaultKeys := make([]string, 0, len(items))
	for _, item := range items {
		choices = append(choices, UIChoice{
			Key:         item.Name,
			Label:       item.Name,
			Description: item.Description,
		})
		defaultKeys = append(defaultKeys, item.Name)
	}

	selected, err := ui.ChooseMany(
		"Select integrations to enable (Enter for all, comma-separated numbers or names, 0 for none):",
		choices,
		defaultKeys,
	)
	if err != nil {
		return suggestedIntegrationPromptResult{}, err
	}

	action, err := ui.Choose(
		suggestedIntegrationActionPrompt,
		[]UIChoice{
			{
				Key:         string(suggestedIntegrationActionUseNow),
				Label:       "Use selected now",
				Description: "Enable the selected integrations for this launch only.",
			},
			{
				Key:         string(suggestedIntegrationActionAlways),
				Label:       "Always use for this project",
				Description: "Pin the selected integrations and suppress deselected suggestions on future launches.",
			},
			{
				Key:         string(suggestedIntegrationActionNotNow),
				Label:       "Not now",
				Description: "Skip these suggestions for this launch without saving a project preference.",
			},
		},
		suggestedIntegrationActionDefaultChoice(ui),
	)
	if err != nil {
		return suggestedIntegrationPromptResult{}, err
	}

	return suggestedIntegrationPromptResult{
		Action:   suggestedIntegrationAction(action),
		Selected: selected,
	}, nil
}

func suggestedIntegrationActionDefaultChoice(ui *UI) string {
	if ui != nil && ui.YesAll {
		return string(suggestedIntegrationActionUseNow)
	}
	return string(suggestedIntegrationActionAlways)
}

func normalizeSuggestedSelection(available, selected []string) ([]string, error) {
	if len(selected) == 0 {
		return nil, nil
	}

	allowed := stringSet(available)
	normalized := make([]string, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, raw := range selected {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unknown suggested integration %q", name)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		normalized = append(normalized, name)
		seen[name] = struct{}{}
	}
	return normalized, nil
}

func persistSuggestedIntegrationPreferences(projectDir string, presented, selected []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	selected = dedupeStrings(selected)
	selectedSet := stringSet(selected)

	pinned := appendUniqueStrings(cfg.ProjectPinnedIntegrations(projectDir), selected)
	cfg.Integrations.Pinned = upsertPinnedIntegrations(cfg.Integrations.Pinned, projectDir, pinned)

	rejected := filterStrings(cfg.ProjectRejectedIntegrations(projectDir), selectedSet)
	rejected = appendUniqueStrings(rejected, subtractStrings(presented, selectedSet))
	cfg.Integrations.Rejected = upsertRejectedIntegrations(cfg.Integrations.Rejected, projectDir, rejected)

	return saveConfig(cfg)
}

func upsertPinnedIntegrations(pins []IntegrationPin, projectDir string, names []string) []IntegrationPin {
	names = dedupeStrings(names)
	filtered := pins[:0]
	for _, pin := range pins {
		if pin.ProjectDir != projectDir {
			filtered = append(filtered, pin)
		}
	}
	if len(names) == 0 {
		return filtered
	}
	return append(filtered, IntegrationPin{
		ProjectDir:   projectDir,
		Integrations: names,
	})
}

func upsertRejectedIntegrations(rejections []IntegrationRejection, projectDir string, names []string) []IntegrationRejection {
	names = dedupeStrings(names)
	filtered := rejections[:0]
	for _, rejection := range rejections {
		if rejection.ProjectDir != projectDir {
			filtered = append(filtered, rejection)
		}
	}
	if len(names) == 0 {
		return filtered
	}
	return append(filtered, IntegrationRejection{
		ProjectDir:   projectDir,
		Integrations: names,
	})
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var deduped []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	return deduped
}

func appendUniqueStrings(existing, additions []string) []string {
	merged := append([]string(nil), dedupeStrings(existing)...)
	seen := stringSet(merged)
	for _, value := range dedupeStrings(additions) {
		if _, dup := seen[value]; dup {
			continue
		}
		merged = append(merged, value)
		seen[value] = struct{}{}
	}
	return merged
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	return set
}

func subtractStrings(values []string, excluded map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	var filtered []string
	for _, value := range values {
		if _, skip := excluded[value]; skip {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func filterStrings(values []string, excluded map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	var filtered []string
	for _, value := range values {
		if _, skip := excluded[value]; skip {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}
