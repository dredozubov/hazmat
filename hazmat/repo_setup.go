package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	repoProfileFormatVersion      = 1
	repoSetupDenialEvidenceLimit  = 32
	repoSetupDenialEvidenceTTL    = 7 * 24 * time.Hour
	repoSetupDenialLogLookbackPad = 2 * time.Second
)

type repoSetupEffectClass string

const (
	repoSetupEffectClassSafe     repoSetupEffectClass = "safe"
	repoSetupEffectClassExplicit repoSetupEffectClass = "explicit"
)

type repoSetupEffectKind string

const (
	repoSetupEffectReadOnly        repoSetupEffectKind = "read_only"
	repoSetupEffectSnapshotExclude repoSetupEffectKind = "snapshot_exclude"
	repoSetupEffectEnvSelector     repoSetupEffectKind = "env_selector"
	repoSetupEffectWrite           repoSetupEffectKind = "write"
)

type repoSetupEffect struct {
	ID            string
	Class         repoSetupEffectClass
	Kind          repoSetupEffectKind
	Value         string
	ResolvedValue string
	Sources       []string
}

type repoSetupStoredEffects struct {
	ReadOnly         []string `yaml:"read_only,omitempty"`
	SnapshotExcludes []string `yaml:"snapshot_excludes,omitempty"`
	EnvSelectors     []string `yaml:"env_selectors,omitempty"`
	Write            []string `yaml:"write,omitempty"`
}

type repoSetupStoredEvidence struct {
	ID          string               `yaml:"id"`
	Class       repoSetupEffectClass `yaml:"class"`
	Kind        repoSetupEffectKind  `yaml:"kind"`
	Value       string               `yaml:"value"`
	Source      string               `yaml:"source"`
	FirstSeenAt string               `yaml:"first_seen_at,omitempty"`
	LastSeenAt  string               `yaml:"last_seen_at,omitempty"`
}

type repoProfileRecord struct {
	ProjectDir     string                    `yaml:"project"`
	ApprovalHash   string                    `yaml:"approval_hash,omitempty"`
	LastSeenHash   string                    `yaml:"last_seen_hash,omitempty"`
	Remembered     repoSetupStoredEffects    `yaml:"remembered,omitempty"`
	RejectedSafe   []string                  `yaml:"rejected_safe,omitempty"`
	DenialEvidence []repoSetupStoredEvidence `yaml:"denial_evidence,omitempty"`
}

type repoProfileStore struct {
	Version int                 `yaml:"version"`
	Repos   []repoProfileRecord `yaml:"repos,omitempty"`
}

type repoSetupState struct {
	CandidateHash         string
	ApprovalHash          string
	SuggestedIntegrations []string
	AppliedSafe           []repoSetupEffect
	AppliedExplicit       []repoSetupEffect
	PendingSafe           []repoSetupEffect
	PendingExplicit       []repoSetupEffect
	Notes                 []string
	CandidateMutationPlan sessionMutationPlan

	record                 repoProfileRecord
	currentSafe            repoSetupStoredEffects
	currentExplicit        repoSetupStoredEffects
	appliedSafe            repoSetupStoredEffects
	appliedExplicit        repoSetupStoredEffects
	currentSafeEffects     []repoSetupEffect
	currentExplicitEffects []repoSetupEffect
}

var repoSetupLogShow = func(start, end time.Time) (string, error) {
	args := []string{
		"--style", "compact",
		"--start", start.Format("2006-01-02 15:04:05"),
		"--end", end.Format("2006-01-02 15:04:05"),
		"--predicate", `sender == "Sandbox"`,
	}
	out, err := exec.Command(hostLogPath, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func repoProfileStorePath() string {
	return filepath.Join(filepath.Dir(configFilePath), "repo-profiles.yaml")
}

func loadRepoProfileStore() (repoProfileStore, error) {
	store := repoProfileStore{Version: repoProfileFormatVersion}
	data, err := os.ReadFile(repoProfileStorePath())
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return store, fmt.Errorf("read repo profiles: %w", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&store); err != nil {
		return store, fmt.Errorf("parse repo profiles: %w", err)
	}
	if store.Version == 0 {
		store.Version = repoProfileFormatVersion
	}
	return store, nil
}

func saveRepoProfileStore(store repoProfileStore) error {
	store.Version = repoProfileFormatVersion
	dir := filepath.Dir(repoProfileStorePath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create repo profile dir: %w", err)
	}
	data, err := yaml.Marshal(&store)
	if err != nil {
		return fmt.Errorf("marshal repo profiles: %w", err)
	}
	header := "# Hazmat repo setup profiles\n# Host-owned remembered onboarding state.\n\n"
	return os.WriteFile(repoProfileStorePath(), []byte(header+string(data)), 0o600)
}

func loadRepoProfileRecord(projectDir string) (repoProfileRecord, error) {
	store, err := loadRepoProfileStore()
	if err != nil {
		return repoProfileRecord{}, err
	}
	for _, record := range store.Repos {
		if record.ProjectDir == projectDir {
			record.Remembered = record.Remembered.normalized()
			record.RejectedSafe = dedupeStrings(record.RejectedSafe)
			record.DenialEvidence = normalizeRepoSetupEvidence(record.DenialEvidence)
			return record, nil
		}
	}
	return repoProfileRecord{ProjectDir: projectDir}, nil
}

func saveRepoProfileRecord(record repoProfileRecord) error {
	record.Remembered = record.Remembered.normalized()
	record.RejectedSafe = dedupeStrings(record.RejectedSafe)
	record.DenialEvidence = normalizeRepoSetupEvidence(record.DenialEvidence)

	store, err := loadRepoProfileStore()
	if err != nil {
		return err
	}

	filtered := store.Repos[:0]
	for _, existing := range store.Repos {
		if existing.ProjectDir != record.ProjectDir {
			filtered = append(filtered, existing)
		}
	}
	if !repoProfileRecordEmpty(record) {
		filtered = append(filtered, record)
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].ProjectDir < filtered[j].ProjectDir
		})
	}
	store.Repos = filtered
	return saveRepoProfileStore(store)
}

func repoProfileRecordEmpty(record repoProfileRecord) bool {
	return record.ProjectDir == "" ||
		(record.Remembered.empty() &&
			record.ApprovalHash == "" &&
			record.LastSeenHash == "" &&
			len(record.RejectedSafe) == 0 &&
			len(record.DenialEvidence) == 0)
}

func normalizeRepoSetupEvidence(entries []repoSetupStoredEvidence) []repoSetupStoredEvidence {
	if len(entries) == 0 {
		return nil
	}
	seen := make(map[string]repoSetupStoredEvidence, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.Value == "" {
			continue
		}
		current := entry
		if existing, ok := seen[entry.ID]; ok {
			if existing.FirstSeenAt == "" || (current.FirstSeenAt != "" && current.FirstSeenAt < existing.FirstSeenAt) {
				existing.FirstSeenAt = current.FirstSeenAt
			}
			if current.LastSeenAt > existing.LastSeenAt {
				existing.LastSeenAt = current.LastSeenAt
			}
			seen[entry.ID] = existing
			continue
		}
		seen[entry.ID] = current
	}

	normalized := make([]repoSetupStoredEvidence, 0, len(seen))
	for _, entry := range seen {
		normalized = append(normalized, entry)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].LastSeenAt == normalized[j].LastSeenAt {
			return normalized[i].ID < normalized[j].ID
		}
		return normalized[i].LastSeenAt > normalized[j].LastSeenAt
	})
	if len(normalized) > repoSetupDenialEvidenceLimit {
		normalized = normalized[:repoSetupDenialEvidenceLimit]
	}
	return normalized
}

func (effects repoSetupStoredEffects) normalized() repoSetupStoredEffects {
	return repoSetupStoredEffects{
		ReadOnly:         dedupeAndSortStrings(effects.ReadOnly),
		SnapshotExcludes: dedupeAndSortStrings(effects.SnapshotExcludes),
		EnvSelectors:     dedupeAndSortStrings(effects.EnvSelectors),
		Write:            dedupeAndSortStrings(effects.Write),
	}
}

func (effects repoSetupStoredEffects) empty() bool {
	effects = effects.normalized()
	return len(effects.ReadOnly) == 0 &&
		len(effects.SnapshotExcludes) == 0 &&
		len(effects.EnvSelectors) == 0 &&
		len(effects.Write) == 0
}

func (effects repoSetupStoredEffects) hash() string {
	normalized := effects.normalized()
	data, _ := json.Marshal(struct {
		Version int                    `json:"version"`
		Effects repoSetupStoredEffects `json:"effects"`
	}{
		Version: repoProfileFormatVersion,
		Effects: normalized,
	})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func repoSetupCandidateHash(effects []repoSetupEffect) string {
	normalized := repoSetupMergeEffects(effects)
	payload := struct {
		Version int `json:"version"`
		Effects []struct {
			ID      string               `json:"id"`
			Class   repoSetupEffectClass `json:"class"`
			Kind    repoSetupEffectKind  `json:"kind"`
			Value   string               `json:"value"`
			Sources []string             `json:"sources,omitempty"`
		} `json:"effects,omitempty"`
	}{
		Version: repoProfileFormatVersion,
	}
	for _, effect := range normalized {
		payload.Effects = append(payload.Effects, struct {
			ID      string               `json:"id"`
			Class   repoSetupEffectClass `json:"class"`
			Kind    repoSetupEffectKind  `json:"kind"`
			Value   string               `json:"value"`
			Sources []string             `json:"sources,omitempty"`
		}{
			ID:      effect.ID,
			Class:   effect.Class,
			Kind:    effect.Kind,
			Value:   effect.Value,
			Sources: dedupeAndSortStrings(effect.Sources),
		})
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (effects repoSetupStoredEffects) ids() map[string]struct{} {
	normalized := effects.normalized()
	ids := make(map[string]struct{}, len(normalized.ReadOnly)+len(normalized.SnapshotExcludes)+len(normalized.EnvSelectors)+len(normalized.Write))
	for _, value := range normalized.ReadOnly {
		ids["ro:"+value] = struct{}{}
	}
	for _, value := range normalized.SnapshotExcludes {
		ids["exclude:"+value] = struct{}{}
	}
	for _, value := range normalized.EnvSelectors {
		ids["env:"+value] = struct{}{}
	}
	for _, value := range normalized.Write {
		ids["rw:"+value] = struct{}{}
	}
	return ids
}

func (effects repoSetupStoredEffects) intersection(other repoSetupStoredEffects) repoSetupStoredEffects {
	otherIDs := other.ids()
	var result repoSetupStoredEffects
	for _, value := range effects.normalized().ReadOnly {
		if _, ok := otherIDs["ro:"+value]; ok {
			result.ReadOnly = append(result.ReadOnly, value)
		}
	}
	for _, value := range effects.normalized().SnapshotExcludes {
		if _, ok := otherIDs["exclude:"+value]; ok {
			result.SnapshotExcludes = append(result.SnapshotExcludes, value)
		}
	}
	for _, value := range effects.normalized().EnvSelectors {
		if _, ok := otherIDs["env:"+value]; ok {
			result.EnvSelectors = append(result.EnvSelectors, value)
		}
	}
	for _, value := range effects.normalized().Write {
		if _, ok := otherIDs["rw:"+value]; ok {
			result.Write = append(result.Write, value)
		}
	}
	return result.normalized()
}

func (effects repoSetupStoredEffects) union(other repoSetupStoredEffects) repoSetupStoredEffects {
	return repoSetupStoredEffects{
		ReadOnly:         append(effects.ReadOnly, other.ReadOnly...),
		SnapshotExcludes: append(effects.SnapshotExcludes, other.SnapshotExcludes...),
		EnvSelectors:     append(effects.EnvSelectors, other.EnvSelectors...),
		Write:            append(effects.Write, other.Write...),
	}.normalized()
}

func (effects repoSetupStoredEffects) subsetOf(other repoSetupStoredEffects) bool {
	otherIDs := other.ids()
	for id := range effects.ids() {
		if _, ok := otherIDs[id]; !ok {
			return false
		}
	}
	return true
}

func repoSetupStoredEffectsFromEffects(effects []repoSetupEffect) repoSetupStoredEffects {
	var stored repoSetupStoredEffects
	for _, effect := range effects {
		switch effect.Kind {
		case repoSetupEffectReadOnly:
			stored.ReadOnly = append(stored.ReadOnly, effect.Value)
		case repoSetupEffectSnapshotExclude:
			stored.SnapshotExcludes = append(stored.SnapshotExcludes, effect.Value)
		case repoSetupEffectEnvSelector:
			stored.EnvSelectors = append(stored.EnvSelectors, effect.Value)
		case repoSetupEffectWrite:
			stored.Write = append(stored.Write, effect.Value)
		}
	}
	return stored.normalized()
}

func repoSetupEffectKindsSummary(effects []repoSetupEffect) string {
	if len(effects) == 0 {
		return ""
	}
	counts := map[repoSetupEffectKind]int{}
	for _, effect := range effects {
		counts[effect.Kind]++
	}
	var parts []string
	if count := counts[repoSetupEffectReadOnly]; count > 0 {
		parts = append(parts, pluralizeRepoSetup(count, "read-only path"))
	}
	if count := counts[repoSetupEffectWrite]; count > 0 {
		parts = append(parts, pluralizeRepoSetup(count, "write path"))
	}
	if count := counts[repoSetupEffectSnapshotExclude]; count > 0 {
		parts = append(parts, pluralizeRepoSetup(count, "snapshot exclude"))
	}
	if count := counts[repoSetupEffectEnvSelector]; count > 0 {
		parts = append(parts, pluralizeRepoSetup(count, "env selector"))
	}
	return strings.Join(parts, ", ")
}

func pluralizeRepoSetup(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func dedupeAndSortStrings(values []string) []string {
	values = dedupeStrings(values)
	sort.Strings(values)
	return values
}

func repoSetupEffectByID(effects []repoSetupEffect) map[string]repoSetupEffect {
	result := make(map[string]repoSetupEffect, len(effects))
	for _, effect := range effects {
		if existing, ok := result[effect.ID]; ok {
			existing.Sources = append(existing.Sources, effect.Sources...)
			existing.Sources = dedupeAndSortStrings(existing.Sources)
			if existing.ResolvedValue == "" {
				existing.ResolvedValue = effect.ResolvedValue
			}
			result[effect.ID] = existing
			continue
		}
		effect.Sources = dedupeAndSortStrings(effect.Sources)
		result[effect.ID] = effect
	}
	return result
}

func repoSetupMergeEffects(lists ...[]repoSetupEffect) []repoSetupEffect {
	merged := map[string]repoSetupEffect{}
	for _, list := range lists {
		for _, effect := range list {
			existing, ok := merged[effect.ID]
			if !ok {
				effect.Sources = dedupeAndSortStrings(effect.Sources)
				merged[effect.ID] = effect
				continue
			}
			existing.Sources = dedupeAndSortStrings(append(existing.Sources, effect.Sources...))
			if existing.ResolvedValue == "" {
				existing.ResolvedValue = effect.ResolvedValue
			}
			merged[effect.ID] = existing
		}
	}
	result := make([]repoSetupEffect, 0, len(merged))
	for _, effect := range merged {
		result = append(result, effect)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].Value < result[j].Value
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}

func repoSetupFilterEffects(effects []repoSetupEffect, allowed repoSetupStoredEffects) []repoSetupEffect {
	if allowed.empty() || len(effects) == 0 {
		return nil
	}
	ids := allowed.ids()
	filtered := make([]repoSetupEffect, 0, len(effects))
	for _, effect := range effects {
		if _, ok := ids[effect.ID]; ok {
			filtered = append(filtered, effect)
		}
	}
	return filtered
}

func repoSetupSubtractEffects(effects []repoSetupEffect, removedIDs map[string]struct{}) []repoSetupEffect {
	if len(effects) == 0 {
		return nil
	}
	filtered := make([]repoSetupEffect, 0, len(effects))
	for _, effect := range effects {
		if _, removed := removedIDs[effect.ID]; removed {
			continue
		}
		filtered = append(filtered, effect)
	}
	return filtered
}

func repoSetupRejectedIDs(values []string) map[string]struct{} {
	rejected := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		rejected[value] = struct{}{}
	}
	return rejected
}

func repoSetupStateForSession(cfg sessionConfig) (repoSetupState, error) {
	record, err := loadRepoProfileRecord(cfg.ProjectDir)
	if err != nil {
		return repoSetupState{}, err
	}

	safeSuggestionEffects, candidatePlan, err := repoSetupEffectsFromSuggestedIntegrations(cfg.ProjectDir, cfg.SuggestedIntegrations)
	if err != nil {
		return repoSetupState{}, err
	}
	safeDenialEffects, explicitDenialEffects := repoSetupEffectsFromEvidence(record.DenialEvidence)

	currentSafeEffects := repoSetupMergeEffects(safeSuggestionEffects, safeDenialEffects)
	currentExplicitEffects := repoSetupMergeEffects(explicitDenialEffects)
	currentSafe := repoSetupStoredEffectsFromEffects(currentSafeEffects)
	currentExplicit := repoSetupStoredEffectsFromEffects(currentExplicitEffects)

	suggestionSafe := repoSetupStoredEffectsFromEffects(safeSuggestionEffects)
	denialSafe := repoSetupStoredEffectsFromEffects(safeDenialEffects)

	var appliedSafe repoSetupStoredEffects
	if suggestionSafe.subsetOf(record.Remembered) {
		appliedSafe = appliedSafe.union(suggestionSafe)
	}
	appliedSafe = appliedSafe.union(denialSafe.intersection(record.Remembered))
	appliedExplicit := currentExplicit.intersection(record.Remembered)

	rejectedIDs := repoSetupRejectedIDs(record.RejectedSafe)
	appliedIDs := appliedSafe.union(appliedExplicit).ids()
	for id := range appliedIDs {
		rejectedIDs[id] = struct{}{}
	}

	pendingSafe := repoSetupSubtractEffects(currentSafeEffects, rejectedIDs)
	pendingExplicit := repoSetupSubtractEffects(currentExplicitEffects, appliedIDs)

	candidateAll := repoSetupMergeEffects(currentSafeEffects, currentExplicitEffects)
	state := repoSetupState{
		CandidateHash:          repoSetupCandidateHash(candidateAll),
		ApprovalHash:           record.Remembered.hash(),
		SuggestedIntegrations:  append([]string(nil), cfg.SuggestedIntegrations...),
		AppliedSafe:            repoSetupFilterEffects(currentSafeEffects, appliedSafe),
		AppliedExplicit:        repoSetupFilterEffects(currentExplicitEffects, appliedExplicit),
		PendingSafe:            pendingSafe,
		PendingExplicit:        pendingExplicit,
		CandidateMutationPlan:  candidatePlan,
		record:                 record,
		currentSafe:            currentSafe,
		currentExplicit:        currentExplicit,
		appliedSafe:            appliedSafe,
		appliedExplicit:        appliedExplicit,
		currentSafeEffects:     currentSafeEffects,
		currentExplicitEffects: currentExplicitEffects,
	}
	return state, nil
}

func repoSetupEffectsFromSuggestedIntegrations(projectDir string, names []string) ([]repoSetupEffect, sessionMutationPlan, error) {
	if len(names) == 0 {
		return nil, sessionMutationPlan{}, nil
	}
	specs := make([]IntegrationSpec, 0, len(names))
	for _, name := range dedupeAndSortStrings(names) {
		spec, err := loadIntegrationSpecByName(name)
		if err != nil {
			return nil, sessionMutationPlan{}, err
		}
		specs = append(specs, spec)
	}

	resolved, plan, err := resolveRuntimeIntegrations(projectDir, specs)
	if err != nil {
		return nil, sessionMutationPlan{}, err
	}
	merged, err := mergeResolvedIntegrations(resolved)
	if err != nil {
		return nil, sessionMutationPlan{}, err
	}

	source := "Suggested by project files"
	if len(names) > 0 {
		source = fmt.Sprintf("Suggested by project files (%s)", strings.Join(dedupeAndSortStrings(names), ", "))
	}
	var effects []repoSetupEffect
	for _, dir := range merged.ReadDirs {
		effects = append(effects, repoSetupEffect{
			ID:      "ro:" + dir,
			Class:   repoSetupEffectClassSafe,
			Kind:    repoSetupEffectReadOnly,
			Value:   dir,
			Sources: []string{source},
		})
	}
	for _, exclude := range merged.Excludes {
		effects = append(effects, repoSetupEffect{
			ID:      "exclude:" + exclude,
			Class:   repoSetupEffectClassSafe,
			Kind:    repoSetupEffectSnapshotExclude,
			Value:   exclude,
			Sources: []string{source},
		})
	}
	envKeys := integrationEnvKeys(merged.EnvPassthrough)
	for _, key := range envKeys {
		effects = append(effects, repoSetupEffect{
			ID:            "env:" + key,
			Class:         repoSetupEffectClassSafe,
			Kind:          repoSetupEffectEnvSelector,
			Value:         key,
			ResolvedValue: merged.EnvPassthrough[key],
			Sources:       []string{source},
		})
	}
	return effects, plan, nil
}

func repoSetupEffectsFromEvidence(entries []repoSetupStoredEvidence) (safe []repoSetupEffect, explicit []repoSetupEffect) {
	now := time.Now()
	for _, entry := range normalizeRepoSetupEvidence(entries) {
		if entry.LastSeenAt != "" {
			if ts, err := time.Parse(time.RFC3339, entry.LastSeenAt); err == nil && now.Sub(ts) > repoSetupDenialEvidenceTTL {
				continue
			}
		}
		effect := repoSetupEffect{
			ID:      entry.ID,
			Class:   entry.Class,
			Kind:    entry.Kind,
			Value:   entry.Value,
			Sources: []string{entry.Source},
		}
		switch entry.Class {
		case repoSetupEffectClassExplicit:
			explicit = append(explicit, effect)
		default:
			safe = append(safe, effect)
		}
	}
	return repoSetupMergeEffects(safe), repoSetupMergeEffects(explicit)
}

func repoSetupCaptureDenialEvidence(cfg sessionConfig, start, end time.Time) []repoSetupStoredEvidence {
	if end.Before(start) {
		end = start
	}
	start = start.Add(-repoSetupDenialLogLookbackPad)
	end = end.Add(repoSetupDenialLogLookbackPad)
	out, err := repoSetupLogShow(start, end)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	var evidence []repoSetupStoredEvidence
	for _, line := range lines {
		entry, ok := repoSetupEvidenceFromLogLine(cfg, strings.TrimSpace(line), end)
		if !ok {
			continue
		}
		evidence = append(evidence, entry)
	}
	return normalizeRepoSetupEvidence(evidence)
}

func rememberRepoSetupDenials(cfg sessionConfig, start, end time.Time) error {
	evidence := repoSetupCaptureDenialEvidence(cfg, start, end)
	if len(evidence) == 0 {
		return nil
	}
	record, err := loadRepoProfileRecord(cfg.ProjectDir)
	if err != nil {
		return err
	}
	record.ProjectDir = cfg.ProjectDir
	record.DenialEvidence = normalizeRepoSetupEvidence(append(record.DenialEvidence, evidence...))
	return saveRepoProfileRecord(record)
}

func repoSetupEvidenceFromLogLine(cfg sessionConfig, line string, seenAt time.Time) (repoSetupStoredEvidence, bool) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.Contains(strings.ToLower(line), "deny") {
		return repoSetupStoredEvidence{}, false
	}

	operation, path, ok := parseSandboxDenialLine(line)
	if !ok {
		return repoSetupStoredEvidence{}, false
	}

	class, kind, normalizedPath, ok := normalizeDeniedPathToRepoSetupEffect(cfg, operation, path)
	if !ok {
		return repoSetupStoredEvidence{}, false
	}

	idPrefix := "ro:"
	if kind == repoSetupEffectWrite {
		idPrefix = "rw:"
	}

	timestamp := seenAt.Format(time.RFC3339)
	return repoSetupStoredEvidence{
		ID:          idPrefix + normalizedPath,
		Class:       class,
		Kind:        kind,
		Value:       normalizedPath,
		Source:      "Learned from previous session denial",
		FirstSeenAt: timestamp,
		LastSeenAt:  timestamp,
	}, true
}

func parseSandboxDenialLine(line string) (string, string, bool) {
	fields := strings.Fields(line)
	for i, field := range fields {
		if !strings.Contains(field, "deny") {
			continue
		}
		for j := i + 1; j < len(fields)-1; j++ {
			op := fields[j]
			path := strings.Trim(fields[j+1], `"'`)
			if strings.HasPrefix(path, "/") && looksLikeSandboxOperation(op) {
				return op, path, true
			}
		}
	}

	opIdx := strings.Index(line, "operation=")
	pathIdx := strings.Index(line, "path=")
	if opIdx >= 0 && pathIdx >= 0 && pathIdx > opIdx {
		opValue := strings.Fields(line[opIdx+len("operation="):])[0]
		pathValue := strings.Trim(strings.Fields(line[pathIdx+len("path="):])[0], `"'`)
		if pathValue != "" && looksLikeSandboxOperation(opValue) {
			return opValue, pathValue, true
		}
	}
	return "", "", false
}

func looksLikeSandboxOperation(op string) bool {
	return strings.HasPrefix(op, "file-read") ||
		strings.HasPrefix(op, "file-write") ||
		op == "file-map-executable" ||
		op == "file-create" ||
		op == "file-rename"
}

func normalizeDeniedPathToRepoSetupEffect(cfg sessionConfig, operation, rawPath string) (repoSetupEffectClass, repoSetupEffectKind, string, bool) {
	canonical, err := canonicalizePathMaybeMissing(rawPath)
	if err != nil || canonical == "" {
		return "", "", "", false
	}
	if canonical == cfg.ProjectDir || isWithinDir(cfg.ProjectDir, canonical) || isCredentialDenyPath(canonical) {
		return "", "", "", false
	}

	for _, dir := range cfg.ReadDirs {
		if dir == canonical || isWithinDir(dir, canonical) {
			return "", "", "", false
		}
	}
	for _, dir := range cfg.WriteDirs {
		if dir == canonical || isWithinDir(dir, canonical) {
			return "", "", "", false
		}
	}

	normalized := repoSetupNormalizeDeniedRoot(canonical)
	if normalized == "" || isCredentialDenyPath(normalized) {
		return "", "", "", false
	}

	switch {
	case strings.HasPrefix(operation, "file-read"), operation == "file-map-executable":
		return repoSetupEffectClassSafe, repoSetupEffectReadOnly, normalized, true
	case strings.HasPrefix(operation, "file-write"), operation == "file-create", operation == "file-rename":
		return repoSetupEffectClassExplicit, repoSetupEffectWrite, normalized, true
	default:
		return "", "", "", false
	}
}

func repoSetupNormalizeDeniedRoot(canonical string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(canonical, home+string(os.PathSeparator)) {
		rel := strings.TrimPrefix(canonical, home+string(os.PathSeparator))
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) == 0 {
			return ""
		}
		switch parts[0] {
		case ".cache", ".config", ".local":
			if len(parts) >= 2 {
				return filepath.Join(home, parts[0], parts[1])
			}
		case ".cargo", ".gradle", ".m2", ".npm", ".pnpm-store", ".rustup", ".ivy2":
			return filepath.Join(home, parts[0])
		}
		if strings.HasPrefix(parts[0], ".") {
			return filepath.Join(home, parts[0])
		}
	}

	for _, root := range integrationGenericToolchainRoots {
		if canonical == root || strings.HasPrefix(canonical, root+string(os.PathSeparator)) {
			rel := strings.TrimPrefix(canonical, root)
			rel = strings.TrimPrefix(rel, string(os.PathSeparator))
			parts := strings.Split(rel, string(os.PathSeparator))
			if len(parts) >= 3 && parts[0] == "Cellar" {
				return filepath.Join(root, parts[0], parts[1], parts[2])
			}
			if len(parts) >= 2 && parts[0] == "opt" {
				return filepath.Join(root, parts[0], parts[1])
			}
			return root
		}
	}

	parent := filepath.Dir(canonical)
	if parent != "" && parent != "." && parent != "/" {
		return parent
	}
	return ""
}

func canonicalizePathMaybeMissing(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(abs)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return abs, nil
	}
	return filepath.Join(resolvedParent, filepath.Base(abs)), nil
}
