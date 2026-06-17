package hazmat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type harnessAssetKind string

const (
	harnessAssetFileRoot harnessAssetKind = "file-root"
	harnessAssetDirRoot  harnessAssetKind = "dir-root"

	harnessAssetEntryFile = "file"
	harnessAssetEntryDir  = "dir"

	harnessAssetsStateVersion = 1
	harnessAssetWarningLimit  = 5
)

type harnessAssetSpec struct {
	Harness   HarnessID
	Key       string
	Kind      harnessAssetKind
	HostPath  string
	AgentPath string
	destRoot  string
}

type harnessAssetsState struct {
	Version   int                                      `json:"version"`
	Harnesses map[HarnessID]harnessAssetHarnessEntries `json:"harnesses,omitempty"`
}

type harnessAssetHarnessEntries struct {
	Entries map[string]harnessAssetManifestEntry `json:"entries,omitempty"`
}

type harnessAssetManifestEntry struct {
	SpecKey     string `json:"spec_key"`
	DestPath    string `json:"dest_path"`
	SourcePath  string `json:"source_path"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	ManagedAt   string `json:"managed_at"`
}

type harnessAssetDesiredEntry struct {
	Spec        harnessAssetSpec
	SourcePath  string
	DestPath    string
	Kind        string
	Fingerprint string
	SourceState harnessAssetPathSnapshot
}

type harnessAssetSyncResult struct {
	Added     int
	Updated   int
	Adopted   int
	Deleted   int
	Conflicts int
	Warnings  []string
}

type harnessAssetSyncPreview struct {
	Desired  map[string]harnessAssetDesiredEntry
	Result   harnessAssetSyncResult
	Warnings []string
}

type harnessAssetPathSnapshot struct {
	Entries map[string]harnessAssetPathSnapshotEntry
}

type harnessAssetPathSnapshotEntry struct {
	Kind            string
	Mode            uint32
	Size            int64
	ModTimeUnixNano int64
}

var (
	harnessAssetsFilePath              = filepath.Join(os.Getenv("HOME"), ".hazmat/harness-assets.json")
	harnessAssetAgentHome              = agentHome
	harnessAssetsNow                   = func() time.Time { return time.Now().UTC() }
	collectDesiredHarnessAssetsForSync = collectDesiredHarnessAssets
	harnessAssetUseAgentBackend        = defaultHarnessAssetUseAgentBackend
	harnessAssetAgentEnsureDir         = defaultHarnessAssetAgentEnsureDir
	harnessAssetAgentWriteFile         = agentWriteFile
	harnessAssetAgentPathExists        = agentPathExists
	harnessAssetAgentRename            = defaultHarnessAssetAgentRename
	harnessAssetAgentRemoveAll         = defaultHarnessAssetAgentRemoveAll
	harnessAssetAgentDirWritable       = defaultHarnessAssetAgentDirWritable
	harnessAssetAgentRepairDir         = defaultHarnessAssetAgentRepairDir
	harnessAssetPathForDirectIO        = func(path string) string { return path }
	harnessAssetSpecs                  = map[HarnessID][]harnessAssetSpec{
		HarnessClaude: {
			{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: "~/.claude/CLAUDE.md", AgentPath: agentHome + "/.claude/CLAUDE.md"},
			{Harness: HarnessClaude, Key: "commands", Kind: harnessAssetDirRoot, HostPath: "~/.claude/commands", AgentPath: agentHome + "/.claude/commands"},
			{Harness: HarnessClaude, Key: "skills", Kind: harnessAssetDirRoot, HostPath: "~/.claude/skills", AgentPath: agentHome + "/.claude/skills"},
			{Harness: HarnessClaude, Key: "agents", Kind: harnessAssetDirRoot, HostPath: "~/.claude/agents", AgentPath: agentHome + "/.claude/agents"},
		},
		HarnessCodex: {
			{Harness: HarnessCodex, Key: "agents-md", Kind: harnessAssetFileRoot, HostPath: "~/.codex/AGENTS.md", AgentPath: agentHome + "/.codex/AGENTS.md"},
			{Harness: HarnessCodex, Key: "prompts", Kind: harnessAssetDirRoot, HostPath: "~/.codex/prompts", AgentPath: agentHome + "/.codex/prompts"},
			{Harness: HarnessCodex, Key: "rules", Kind: harnessAssetDirRoot, HostPath: "~/.codex/rules", AgentPath: agentHome + "/.codex/rules"},
			{Harness: HarnessCodex, Key: "skills", Kind: harnessAssetDirRoot, HostPath: "~/.agents/skills", AgentPath: agentHome + "/.agents/skills"},
		},
		HarnessOpenCode: {
			{Harness: HarnessOpenCode, Key: "commands", Kind: harnessAssetDirRoot, HostPath: "~/.config/opencode/commands", AgentPath: agentHome + "/.config/opencode/commands"},
			{Harness: HarnessOpenCode, Key: "agents", Kind: harnessAssetDirRoot, HostPath: "~/.config/opencode/agents", AgentPath: agentHome + "/.config/opencode/agents"},
			{Harness: HarnessOpenCode, Key: "skills", Kind: harnessAssetDirRoot, HostPath: "~/.config/opencode/skills", AgentPath: agentHome + "/.config/opencode/skills"},
		},
		HarnessGemini: {
			{Harness: HarnessGemini, Key: "gemini-md", Kind: harnessAssetFileRoot, HostPath: "~/.gemini/GEMINI.md", AgentPath: agentHome + "/.gemini/GEMINI.md"},
			{Harness: HarnessGemini, Key: "extensions", Kind: harnessAssetDirRoot, HostPath: "~/.gemini/extensions", AgentPath: agentHome + "/.gemini/extensions"},
		},
		HarnessQwen: {
			{Harness: HarnessQwen, Key: "qwen-md", Kind: harnessAssetFileRoot, HostPath: "~/.qwen/QWEN.md", AgentPath: agentHome + "/.qwen/QWEN.md"},
			{Harness: HarnessQwen, Key: "extensions", Kind: harnessAssetDirRoot, HostPath: "~/.qwen/extensions", AgentPath: agentHome + "/.qwen/extensions"},
		},
	}
)

func (r harnessAssetSyncResult) hasWork() bool {
	return r.Added > 0 || r.Updated > 0 || r.Adopted > 0 || r.Deleted > 0 || r.Conflicts > 0 || len(r.Warnings) > 0
}

func (r harnessAssetSyncResult) changeSummary() string {
	var parts []string
	if r.Added > 0 {
		parts = append(parts, fmt.Sprintf("%d added", r.Added))
	}
	if r.Updated > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", r.Updated))
	}
	if r.Adopted > 0 {
		parts = append(parts, fmt.Sprintf("%d adopted", r.Adopted))
	}
	if r.Deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", r.Deleted))
	}
	if r.Conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", r.Conflicts))
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

func harnessAssetsEnabled() bool {
	cfg, _ := loadConfig()
	return cfg.HarnessAssets()
}

func harnessIDForCommand(commandName string) (HarnessID, bool) {
	switch commandName {
	case string(HarnessClaude):
		return HarnessClaude, true
	case string(HarnessCodex):
		return HarnessCodex, true
	case string(HarnessOpenCode):
		return HarnessOpenCode, true
	case string(HarnessGemini):
		return HarnessGemini, true
	case string(HarnessHermes):
		return HarnessHermes, true
	case string(HarnessQwen):
		return HarnessQwen, true
	case string(HarnessCursorAgent):
		return HarnessCursorAgent, true
	case string(HarnessPi):
		return HarnessPi, true
	default:
		return "", false
	}
}

func buildHarnessAssetSessionMutationPlan(cfg sessionConfig, commandName string, mode sessionMode, opts harnessSessionOpts) (sessionMutationPlan, error) {
	if mode != sessionModeNative || opts.skipHarnessAssetsSync || !harnessAssetsEnabled() {
		return sessionMutationPlan{}, nil
	}

	harnessID, ok := harnessIDForCommand(commandName)
	if !ok {
		return sessionMutationPlan{}, nil
	}

	destRoot := harnessAssetDestinationRoot(cfg)
	sessionLocal := cfg.SessionHome != nil
	preview, err := previewHarnessAssetSyncForDestination(harnessID, destRoot, sessionLocal)
	if err != nil {
		return sessionMutationPlan{}, err
	}
	if !preview.Result.hasWork() {
		return sessionMutationPlan{}, nil
	}

	harness, ok := managedHarnessByID(harnessID)
	displayName := string(harnessID)
	if ok && strings.TrimSpace(harness.Spec.DisplayName) != "" {
		displayName = harness.Spec.DisplayName
	}

	detail := fmt.Sprintf("may refresh managed prompt assets for %s under %s (%s)", displayName, destRoot, preview.Result.changeSummary())
	if !sessionLocal {
		detail = fmt.Sprintf("may refresh managed prompt assets and repair managed asset directory ownership for %s under %s (%s)", displayName, destRoot, preview.Result.changeSummary())
	}

	plan := sessionMutationPlan{}
	plan.Mutations = append(plan.Mutations, plannedSessionMutation{
		Metadata: sessionMutation{
			Summary:     fmt.Sprintf("%s asset sync", displayName),
			Detail:      detail,
			Persistence: harnessAssetMutationPersistence(sessionLocal),
			ProofScope:  sessionMutationProofScopeTestsDocs,
		},
		Apply: func() (sessionMutationExecution, error) {
			var result harnessAssetSyncResult
			var err error
			if sessionLocal {
				result, err = syncEphemeralHarnessAssetsWithPreview(harnessID, destRoot, preview)
			} else {
				result, err = syncHarnessAssetsWithPreview(harnessID, preview)
			}
			if err != nil {
				return sessionMutationExecution{}, err
			}

			exec := sessionMutationExecution{}
			if result.Added > 0 || result.Updated > 0 || result.Adopted > 0 || result.Deleted > 0 {
				exec.AppliedMessage = fmt.Sprintf("  Synced %s assets (%s)", displayName, result.changeSummary())
			}
			if len(result.Warnings) > 0 {
				exec.Warning = summarizeHarnessAssetWarnings(result.Warnings)
			}
			return exec, nil
		},
	})
	return plan, nil
}

func harnessAssetDestinationRoot(cfg sessionConfig) string {
	if cfg.SessionHome != nil && cfg.SessionHome.Launch.Layout.Home != "" {
		return cfg.SessionHome.Launch.Layout.Home
	}
	return harnessAssetAgentHome
}

func harnessAssetMutationPersistence(sessionLocal bool) string {
	if sessionLocal {
		return "session-local home only"
	}
	return "persistent in agent home"
}

func previewHarnessAssetSync(harnessID HarnessID) (harnessAssetSyncPreview, error) {
	return previewHarnessAssetSyncForDestination(harnessID, harnessAssetAgentHome, false)
}

func previewHarnessAssetSyncForDestination(harnessID HarnessID, destRoot string, sessionLocal bool) (harnessAssetSyncPreview, error) {
	desired, warnings, err := collectDesiredHarnessAssetsForPreview(harnessID, destRoot, sessionLocal)
	if err != nil {
		return harnessAssetSyncPreview{}, err
	}
	if sessionLocal {
		result, err := diffEphemeralHarnessAssetState(desired)
		if err != nil {
			return harnessAssetSyncPreview{}, err
		}
		result.Warnings = append(result.Warnings, warnings...)
		return harnessAssetSyncPreview{
			Desired:  desired,
			Result:   result,
			Warnings: append([]string(nil), warnings...),
		}, nil
	}

	state, err := loadHarnessAssetsState()
	if err != nil {
		return harnessAssetSyncPreview{}, err
	}
	result, err := diffHarnessAssetState(state.harnessEntries(harnessID), desired)
	if err != nil {
		return harnessAssetSyncPreview{}, err
	}
	result.Warnings = append(result.Warnings, warnings...)
	return harnessAssetSyncPreview{
		Desired:  desired,
		Result:   result,
		Warnings: append([]string(nil), warnings...),
	}, nil
}

func collectDesiredHarnessAssetsForPreview(harnessID HarnessID, destRoot string, sessionLocal bool) (map[string]harnessAssetDesiredEntry, []string, error) {
	if !sessionLocal && filepath.Clean(destRoot) == filepath.Clean(harnessAssetAgentHome) {
		return collectDesiredHarnessAssetsForSync(harnessID)
	}
	return collectDesiredHarnessAssetsForDestination(harnessID, destRoot)
}

func syncHarnessAssets(harnessID HarnessID) (harnessAssetSyncResult, error) {
	return syncHarnessAssetsWithPreview(harnessID, harnessAssetSyncPreview{})
}

func syncEphemeralHarnessAssetsWithPreview(harnessID HarnessID, destRoot string, preview harnessAssetSyncPreview) (harnessAssetSyncResult, error) {
	var result harnessAssetSyncResult
	desired := preview.Desired
	warnings := append([]string(nil), preview.Warnings...)
	var err error
	if desired == nil || !harnessAssetPreviewIsFresh(desired) {
		desired, warnings, err = collectDesiredHarnessAssetsForDestination(harnessID, destRoot)
		if err != nil {
			return result, err
		}
	}
	result.Warnings = append(result.Warnings, warnings...)

	for _, destPath := range sortedDesiredHarnessAssetDestPaths(desired) {
		entry := desired[destPath]
		match, err := harnessAssetDestMatches(destPath, entry)
		switch {
		case os.IsNotExist(err):
			if err := installHarnessAssetEntry(entry); err != nil {
				recordSkippedHarnessAssetInstall(&result, destPath, err)
				continue
			}
			result.Added++
		case err == nil && match:
			result.Adopted++
		default:
			result.Conflicts++
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("skipped unmanaged asset %s: %v", destPath, err))
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("skipped unmanaged asset %s because it differs from the managed source", destPath))
			}
		}
	}

	return result, nil
}

func syncHarnessAssetsWithPreview(harnessID HarnessID, preview harnessAssetSyncPreview) (harnessAssetSyncResult, error) {
	var result harnessAssetSyncResult

	if err := withHarnessAssetLock(func() error {
		state, err := loadHarnessAssetsState()
		if err != nil {
			return err
		}

		desired := preview.Desired
		warnings := append([]string(nil), preview.Warnings...)
		if desired == nil || !harnessAssetPreviewIsFresh(desired) {
			desired, warnings, err = collectDesiredHarnessAssetsForSync(harnessID)
			if err != nil {
				return err
			}
		}
		result.Warnings = append(result.Warnings, warnings...)

		harnessState := state.harnessEntries(harnessID)
		if harnessState.Entries == nil {
			harnessState.Entries = make(map[string]harnessAssetManifestEntry)
		}

		destPaths := sortedDesiredHarnessAssetDestPaths(desired)
		for _, destPath := range destPaths {
			entry := desired[destPath]
			manifestEntry, managed := harnessState.Entries[destPath]
			if managed {
				match, err := harnessAssetDestMatches(destPath, entry)
				if err == nil && match {
					continue
				}
				_, statErr := os.Lstat(destPath)
				existedBefore := statErr == nil
				if err := installHarnessAssetEntry(entry); err != nil {
					recordSkippedHarnessAssetInstall(&result, destPath, err)
					continue
				}
				if existedBefore {
					result.Updated++
				} else {
					result.Added++
				}
				harnessState.Entries[destPath] = harnessAssetManifestEntry{
					SpecKey:     entry.Spec.Key,
					DestPath:    entry.DestPath,
					SourcePath:  entry.SourcePath,
					Kind:        entry.Kind,
					Fingerprint: entry.Fingerprint,
					ManagedAt:   harnessAssetsNow().Format(time.RFC3339),
				}
				if manifestEntry.Fingerprint == entry.Fingerprint && err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("repaired missing or modified managed asset %s", destPath))
				}
				continue
			}

			match, err := harnessAssetDestMatches(destPath, entry)
			switch {
			case os.IsNotExist(err):
				if err := installHarnessAssetEntry(entry); err != nil {
					recordSkippedHarnessAssetInstall(&result, destPath, err)
					continue
				}
				result.Added++
				harnessState.Entries[destPath] = harnessAssetManifestEntry{
					SpecKey:     entry.Spec.Key,
					DestPath:    entry.DestPath,
					SourcePath:  entry.SourcePath,
					Kind:        entry.Kind,
					Fingerprint: entry.Fingerprint,
					ManagedAt:   harnessAssetsNow().Format(time.RFC3339),
				}
			case err == nil && match:
				result.Adopted++
				harnessState.Entries[destPath] = harnessAssetManifestEntry{
					SpecKey:     entry.Spec.Key,
					DestPath:    entry.DestPath,
					SourcePath:  entry.SourcePath,
					Kind:        entry.Kind,
					Fingerprint: entry.Fingerprint,
					ManagedAt:   harnessAssetsNow().Format(time.RFC3339),
				}
			default:
				result.Conflicts++
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("skipped unmanaged asset %s: %v", destPath, err))
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("skipped unmanaged asset %s because it differs from the managed source", destPath))
				}
			}
		}

		for destPath := range harnessState.Entries {
			if _, ok := desired[destPath]; ok {
				continue
			}
			if err := removeHarnessAssetPath(destPath); err != nil {
				return err
			}
			delete(harnessState.Entries, destPath)
			result.Deleted++
		}

		if len(harnessState.Entries) == 0 {
			delete(state.Harnesses, harnessID)
		} else {
			if state.Harnesses == nil {
				state.Harnesses = make(map[HarnessID]harnessAssetHarnessEntries)
			}
			state.Harnesses[harnessID] = harnessState
		}

		return writeHarnessAssetsState(state)
	}); err != nil {
		return result, err
	}

	return result, nil
}

func recordSkippedHarnessAssetInstall(result *harnessAssetSyncResult, destPath string, err error) {
	result.Conflicts++
	result.Warnings = append(result.Warnings, fmt.Sprintf("skipped managed asset %s: %v", destPath, err))
}

func diffEphemeralHarnessAssetState(desired map[string]harnessAssetDesiredEntry) (harnessAssetSyncResult, error) {
	return diffHarnessAssetState(harnessAssetHarnessEntries{}, desired)
}

func diffHarnessAssetState(existing harnessAssetHarnessEntries, desired map[string]harnessAssetDesiredEntry) (harnessAssetSyncResult, error) {
	var result harnessAssetSyncResult

	for _, destPath := range sortedDesiredHarnessAssetDestPaths(desired) {
		entry := desired[destPath]
		if _, managed := existing.Entries[destPath]; managed {
			match, err := harnessAssetDestMatches(destPath, entry)
			switch {
			case err == nil && match:
				continue
			case os.IsNotExist(err):
				result.Added++
			default:
				result.Updated++
			}
			continue
		}

		match, err := harnessAssetDestMatches(destPath, entry)
		switch {
		case os.IsNotExist(err):
			result.Added++
		case err == nil && match:
			result.Adopted++
		default:
			result.Conflicts++
		}
	}

	for destPath := range existing.Entries {
		if _, ok := desired[destPath]; !ok {
			result.Deleted++
		}
	}

	return result, nil
}

func collectDesiredHarnessAssets(harnessID HarnessID) (map[string]harnessAssetDesiredEntry, []string, error) {
	return collectDesiredHarnessAssetsForDestination(harnessID, harnessAssetAgentHome)
}

func collectDesiredHarnessAssetsForDestination(harnessID HarnessID, destRoot string) (map[string]harnessAssetDesiredEntry, []string, error) {
	specs, err := harnessAssetSpecsForDestination(harnessID, destRoot)
	if err != nil {
		return nil, nil, err
	}
	desired := make(map[string]harnessAssetDesiredEntry)
	var warnings []string

	for _, spec := range specs {
		entries, entryWarnings, err := collectDesiredHarnessAssetEntries(spec)
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, entryWarnings...)
		for _, entry := range entries {
			desired[entry.DestPath] = entry
		}
	}

	return desired, warnings, nil
}

func harnessAssetSpecsForDestination(harnessID HarnessID, destRoot string) ([]harnessAssetSpec, error) {
	destRoot = filepath.Clean(destRoot)
	if !filepath.IsAbs(destRoot) {
		return nil, fmt.Errorf("harness asset destination root %q must be absolute", destRoot)
	}
	specs := harnessAssetSpecs[harnessID]
	if destRoot == filepath.Clean(harnessAssetAgentHome) {
		return specs, nil
	}
	out := make([]harnessAssetSpec, 0, len(specs))
	for _, spec := range specs {
		cleanAgentPath := filepath.Clean(spec.AgentPath)
		if !isWithinDir(harnessAssetAgentHome, cleanAgentPath) {
			return nil, fmt.Errorf("harness asset destination %q must stay within %s before session-home remap", spec.AgentPath, harnessAssetAgentHome)
		}
		rel, err := filepath.Rel(filepath.Clean(harnessAssetAgentHome), cleanAgentPath)
		if err != nil {
			return nil, fmt.Errorf("compute harness asset relative destination for %q: %w", spec.AgentPath, err)
		}
		spec.AgentPath = filepath.Join(destRoot, rel)
		spec.destRoot = destRoot
		out = append(out, spec)
	}
	return out, nil
}

func collectDesiredHarnessAssetEntries(spec harnessAssetSpec) ([]harnessAssetDesiredEntry, []string, error) {
	switch spec.Kind {
	case harnessAssetFileRoot:
		entry, warning, ok, err := collectDesiredHarnessAssetFile(spec)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			if warning == "" {
				return nil, nil, nil
			}
			return nil, []string{warning}, nil
		}
		if warning != "" {
			return []harnessAssetDesiredEntry{entry}, []string{warning}, nil
		}
		return []harnessAssetDesiredEntry{entry}, nil, nil
	case harnessAssetDirRoot:
		return collectDesiredHarnessAssetDir(spec)
	default:
		return nil, nil, fmt.Errorf("unsupported harness asset kind %q", spec.Kind)
	}
}

func collectDesiredHarnessAssetFile(spec harnessAssetSpec) (harnessAssetDesiredEntry, string, bool, error) {
	allowedDir, exists, warning := resolveHarnessAssetAllowedParent(filepath.Dir(spec.HostPath))
	if !exists {
		return harnessAssetDesiredEntry{}, warning, false, nil
	}

	resolved, info, warning, ok := resolveHarnessAssetTopLevelPath(spec.HostPath, allowedDir)
	if !ok {
		return harnessAssetDesiredEntry{}, warning, false, nil
	}
	if !info.Mode().IsRegular() {
		return harnessAssetDesiredEntry{}, fmt.Sprintf("skipped %s: unsupported source type %s", expandTilde(spec.HostPath), info.Mode().String()), false, nil
	}
	if err := validateHarnessAssetSpecDestPath(spec, spec.AgentPath); err != nil {
		return harnessAssetDesiredEntry{}, "", false, err
	}

	kind, fingerprint, sourceState, err := fingerprintHarnessAssetPathWithSnapshot(resolved)
	if err != nil {
		return harnessAssetDesiredEntry{}, fmt.Sprintf("skipped %s: %v", expandTilde(spec.HostPath), err), false, nil
	}
	return harnessAssetDesiredEntry{
		Spec:        spec,
		SourcePath:  resolved,
		DestPath:    spec.AgentPath,
		Kind:        kind,
		Fingerprint: fingerprint,
		SourceState: sourceState,
	}, warning, true, nil
}

func collectDesiredHarnessAssetDir(spec harnessAssetSpec) ([]harnessAssetDesiredEntry, []string, error) {
	allowedDir, exists, warning := resolveHarnessAssetAllowedParent(filepath.Dir(spec.HostPath))
	if !exists {
		if warning == "" {
			return nil, nil, nil
		}
		return nil, []string{warning}, nil
	}

	rootResolved, info, warning, ok := resolveHarnessAssetTopLevelPath(spec.HostPath, allowedDir)
	if !ok {
		if warning == "" {
			return nil, nil, nil
		}
		return nil, []string{warning}, nil
	}
	if !info.IsDir() {
		return nil, []string{fmt.Sprintf("skipped %s: unsupported source type %s", expandTilde(spec.HostPath), info.Mode().String())}, nil
	}
	if err := validateHarnessAssetSpecDestPath(spec, spec.AgentPath); err != nil {
		return nil, nil, err
	}

	entries, err := os.ReadDir(rootResolved)
	if err != nil {
		return nil, []string{fmt.Sprintf("skipped %s: read directory: %v", rootResolved, err)}, nil
	}

	var desired []harnessAssetDesiredEntry
	var warnings []string
	if warning != "" {
		warnings = append(warnings, warning)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		sourcePath := filepath.Join(spec.HostPath, name)
		resolved, info, warning, ok := resolveHarnessAssetTopLevelPath(sourcePath, rootResolved)
		if !ok {
			if warning != "" {
				warnings = append(warnings, warning)
			}
			continue
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			warnings = append(warnings, fmt.Sprintf("skipped %s: unsupported source type %s", expandTilde(sourcePath), info.Mode().String()))
			continue
		}

		destPath := filepath.Join(spec.AgentPath, name)
		if err := validateHarnessAssetSpecDestPath(spec, destPath); err != nil {
			return nil, nil, err
		}

		kind, fingerprint, sourceState, err := fingerprintHarnessAssetPathWithSnapshot(resolved)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %s: %v", expandTilde(sourcePath), err))
			continue
		}

		desired = append(desired, harnessAssetDesiredEntry{
			Spec:        spec,
			SourcePath:  resolved,
			DestPath:    destPath,
			Kind:        kind,
			Fingerprint: fingerprint,
			SourceState: sourceState,
		})
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}

	return desired, warnings, nil
}

func resolveHarnessAssetAllowedParent(path string) (string, bool, string) {
	expanded := expandTilde(path)
	info, err := os.Stat(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, ""
		}
		return "", false, fmt.Sprintf("skipped %s: stat parent: %v", expanded, err)
	}
	if !info.IsDir() {
		return "", false, fmt.Sprintf("skipped %s: parent is not a directory", expanded)
	}
	canonical, err := canonicalizePath(expanded)
	if err != nil {
		return "", false, fmt.Sprintf("skipped %s: resolve parent: %v", expanded, err)
	}
	return canonical, true, ""
}

func resolveHarnessAssetTopLevelPath(path, allowedRoot string) (string, os.FileInfo, string, bool) {
	expanded := expandTilde(path)
	if _, err := os.Lstat(expanded); err != nil {
		if os.IsNotExist(err) {
			return "", nil, "", false
		}
		return "", nil, fmt.Sprintf("skipped %s: stat source: %v", expanded, err), false
	}

	resolved, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		return "", nil, fmt.Sprintf("skipped %s: resolve symlink: %v", expanded, err), false
	}
	if !isWithinDir(allowedRoot, resolved) {
		return "", nil, fmt.Sprintf("skipped %s: resolved path %s escapes the allowed root %s", expanded, resolved, allowedRoot), false
	}
	if isHostStateDenyPath(resolved) {
		return "", nil, fmt.Sprintf("skipped %s: resolves to host-state deny zone %s", expanded, resolved), false
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, fmt.Sprintf("skipped %s: stat resolved path: %v", expanded, err), false
	}
	return resolved, info, "", true
}

func validateHarnessAssetDestPath(destPath string) error {
	return validateHarnessAssetDestPathWithin(destPath, harnessAssetAgentHome)
}

func validateHarnessAssetSpecDestPath(spec harnessAssetSpec, destPath string) error {
	destRoot := spec.destRoot
	if destRoot == "" {
		destRoot = harnessAssetAgentHome
	}
	return validateHarnessAssetDestPathWithin(destPath, destRoot)
}

func validateHarnessAssetDestPathWithin(destPath, destRoot string) error {
	if strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("empty harness asset destination path")
	}
	destRoot = filepath.Clean(destRoot)
	if !filepath.IsAbs(destRoot) {
		return fmt.Errorf("harness asset destination root %q must be absolute", destRoot)
	}
	if !isWithinDir(destRoot, destPath) {
		return fmt.Errorf("harness asset destination %q must stay within %s", destPath, destRoot)
	}
	return nil
}

func harnessAssetDestMatches(destPath string, entry harnessAssetDesiredEntry) (bool, error) {
	kind, fingerprint, err := fingerprintHarnessAssetPath(harnessAssetPathForDirectIO(destPath))
	if err != nil {
		return false, err
	}
	return kind == entry.Kind && fingerprint == entry.Fingerprint, nil
}

func fingerprintHarnessAssetPath(path string) (string, string, error) {
	kind, fingerprint, _, err := fingerprintHarnessAssetPathWithSnapshot(path)
	return kind, fingerprint, err
}

func fingerprintHarnessAssetPathWithSnapshot(path string) (string, string, harnessAssetPathSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", harnessAssetPathSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", harnessAssetPathSnapshot{}, fmt.Errorf("top-level symlinks are not supported at %s", path)
	}
	if info.Mode().IsRegular() {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", harnessAssetPathSnapshot{}, err
		}
		sum := sha256.Sum256(raw)
		snapshot := harnessAssetPathSnapshot{
			Entries: map[string]harnessAssetPathSnapshotEntry{
				".": harnessAssetSnapshotEntryForInfo(info, harnessAssetEntryFile),
			},
		}
		return harnessAssetEntryFile, "sha256:" + hex.EncodeToString(sum[:]), snapshot, nil
	}
	if !info.IsDir() {
		return "", "", harnessAssetPathSnapshot{}, fmt.Errorf("unsupported source type %s", info.Mode().String())
	}

	sum, snapshot, err := fingerprintHarnessAssetDir(path)
	if err != nil {
		return "", "", harnessAssetPathSnapshot{}, err
	}
	return harnessAssetEntryDir, "sha256:" + hex.EncodeToString(sum[:]), snapshot, nil
}

func fingerprintHarnessAssetDir(path string) ([32]byte, harnessAssetPathSnapshot, error) {
	hasher := sha256.New()
	snapshot := harnessAssetPathSnapshot{Entries: make(map[string]harnessAssetPathSnapshotEntry)}
	if err := hashHarnessAssetDirInto(hasher, snapshot.Entries, path, "."); err != nil {
		return [32]byte{}, harnessAssetPathSnapshot{}, err
	}
	var sum [32]byte
	copy(sum[:], hasher.Sum(nil))
	return sum, snapshot, nil
}

func hashHarnessAssetDirInto(hasher hash.Hash, snapshot map[string]harnessAssetPathSnapshotEntry, path, rel string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("nested symlink %s is not supported", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	snapshot[rel] = harnessAssetSnapshotEntryForInfo(info, harnessAssetEntryDir)
	if _, err := hasher.Write([]byte("dir\x00" + rel + "\x00")); err != nil {
		return err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		child := filepath.Join(path, name)
		info, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("nested symlink %s is not supported", child)
		}

		childRel := filepath.Join(rel, name)
		if info.Mode().IsRegular() {
			snapshot[childRel] = harnessAssetSnapshotEntryForInfo(info, harnessAssetEntryFile)
			raw, err := os.ReadFile(child)
			if err != nil {
				return err
			}
			if _, err := hasher.Write([]byte("file\x00" + childRel + "\x00")); err != nil {
				return err
			}
			if _, err := hasher.Write(raw); err != nil {
				return err
			}
			continue
		}
		if info.IsDir() {
			if err := hashHarnessAssetDirInto(hasher, snapshot, child, childRel); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("unsupported nested source type %s at %s", info.Mode().String(), child)
	}
	return nil
}

func harnessAssetSnapshotEntryForInfo(info os.FileInfo, kind string) harnessAssetPathSnapshotEntry {
	return harnessAssetPathSnapshotEntry{
		Kind:            kind,
		Mode:            uint32(info.Mode()),
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}
}

func harnessAssetPreviewIsFresh(desired map[string]harnessAssetDesiredEntry) bool {
	for _, entry := range desired {
		if !harnessAssetPathSnapshotMatches(entry.SourcePath, entry.SourceState) {
			return false
		}
	}
	return true
}

func harnessAssetPathSnapshotMatches(path string, snapshot harnessAssetPathSnapshot) bool {
	if len(snapshot.Entries) == 0 {
		return false
	}
	current, err := snapshotHarnessAssetPath(path)
	if err != nil {
		return false
	}
	if len(current.Entries) != len(snapshot.Entries) {
		return false
	}
	for rel, expected := range snapshot.Entries {
		actual, ok := current.Entries[rel]
		if !ok || actual != expected {
			return false
		}
	}
	return true
}

func snapshotHarnessAssetPath(path string) (harnessAssetPathSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return harnessAssetPathSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return harnessAssetPathSnapshot{}, fmt.Errorf("top-level symlinks are not supported at %s", path)
	}

	snapshot := harnessAssetPathSnapshot{Entries: make(map[string]harnessAssetPathSnapshotEntry)}
	if info.Mode().IsRegular() {
		snapshot.Entries["."] = harnessAssetSnapshotEntryForInfo(info, harnessAssetEntryFile)
		return snapshot, nil
	}
	if !info.IsDir() {
		return harnessAssetPathSnapshot{}, fmt.Errorf("unsupported source type %s", info.Mode().String())
	}
	if err := snapshotHarnessAssetDirInto(snapshot.Entries, path, "."); err != nil {
		return harnessAssetPathSnapshot{}, err
	}
	return snapshot, nil
}

func snapshotHarnessAssetDirInto(snapshot map[string]harnessAssetPathSnapshotEntry, path, rel string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("nested symlink %s is not supported", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	snapshot[rel] = harnessAssetSnapshotEntryForInfo(info, harnessAssetEntryDir)

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		childInfo, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("nested symlink %s is not supported", child)
		}

		childRel := filepath.Join(rel, entry.Name())
		if childInfo.Mode().IsRegular() {
			snapshot[childRel] = harnessAssetSnapshotEntryForInfo(childInfo, harnessAssetEntryFile)
			continue
		}
		if childInfo.IsDir() {
			if err := snapshotHarnessAssetDirInto(snapshot, child, childRel); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("unsupported nested source type %s at %s", childInfo.Mode().String(), child)
	}
	return nil
}

func sortedDesiredHarnessAssetDestPaths(desired map[string]harnessAssetDesiredEntry) []string {
	paths := make([]string, 0, len(desired))
	for destPath := range desired {
		paths = append(paths, destPath)
	}
	sort.Strings(paths)
	return paths
}

func loadHarnessAssetsState() (harnessAssetsState, error) {
	data, err := os.ReadFile(harnessAssetsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return harnessAssetsState{
				Version:   harnessAssetsStateVersion,
				Harnesses: make(map[HarnessID]harnessAssetHarnessEntries),
			}, nil
		}
		return harnessAssetsState{}, err
	}

	var state harnessAssetsState
	if err := json.Unmarshal(data, &state); err != nil {
		return harnessAssetsState{}, err
	}
	if state.Version == 0 {
		state.Version = harnessAssetsStateVersion
	}
	if state.Version != harnessAssetsStateVersion {
		return harnessAssetsState{}, fmt.Errorf("unsupported harness asset state version %d", state.Version)
	}
	if state.Harnesses == nil {
		state.Harnesses = make(map[HarnessID]harnessAssetHarnessEntries)
	}
	return state, nil
}

func (s harnessAssetsState) harnessEntries(id HarnessID) harnessAssetHarnessEntries {
	if s.Harnesses == nil {
		return harnessAssetHarnessEntries{Entries: make(map[string]harnessAssetManifestEntry)}
	}
	entries, ok := s.Harnesses[id]
	if !ok {
		return harnessAssetHarnessEntries{Entries: make(map[string]harnessAssetManifestEntry)}
	}
	if entries.Entries == nil {
		entries.Entries = make(map[string]harnessAssetManifestEntry)
	}
	return entries
}

func writeHarnessAssetsState(state harnessAssetsState) error {
	if state.Version == 0 {
		state.Version = harnessAssetsStateVersion
	}
	if state.Harnesses == nil {
		state.Harnesses = make(map[HarnessID]harnessAssetHarnessEntries)
	}

	dir := filepath.Dir(harnessAssetsFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(dir, "harness-assets-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, harnessAssetsFilePath)
}

func withHarnessAssetLock(fn func() error) error {
	lockDir := filepath.Join(filepath.Dir(harnessAssetsFilePath), "locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return err
	}

	// The ownership manifest is shared across all harnesses, so updates need a
	// single manifest lock to prevent cross-harness lost updates.
	lockPath := filepath.Join(lockDir, "harness-assets.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}()

	return fn()
}

func summarizeHarnessAssetWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	if len(warnings) == 1 {
		return warnings[0] + "\n    The asset was not copied. Fix the source path or launch with --skip-harness-assets-sync to skip managed asset refresh."
	}

	allEscapes := true
	for _, warning := range warnings {
		if !strings.Contains(warning, "escapes the allowed root") {
			allEscapes = false
			break
		}
	}

	limit := harnessAssetWarningLimit
	if len(warnings) < limit {
		limit = len(warnings)
	}

	var b strings.Builder
	if allEscapes {
		fmt.Fprintf(&b, "%d harness assets were not copied because their symlink targets leave the managed source root.", len(warnings))
	} else {
		fmt.Fprintf(&b, "%d harness asset warning(s); affected assets were not copied.", len(warnings))
	}
	fmt.Fprintf(&b, "\n    Showing %d:", limit)
	for i := 0; i < limit; i++ {
		fmt.Fprintf(&b, "\n    - %s", warnings[i])
	}
	if remaining := len(warnings) - limit; remaining > 0 {
		fmt.Fprintf(&b, "\n    - %d more omitted", remaining)
	}
	b.WriteString("\n    Fix or remove those source entries, or launch with --skip-harness-assets-sync to skip managed asset refresh.")
	return b.String()
}

func installHarnessAssetEntry(entry harnessAssetDesiredEntry) error {
	if err := validateHarnessAssetSpecDestPath(entry.Spec, entry.DestPath); err != nil {
		return err
	}
	if harnessAssetUseAgentBackend(entry.DestPath) {
		return installHarnessAssetEntryAsAgent(entry)
	}
	if err := os.MkdirAll(filepath.Dir(entry.DestPath), 0o2770); err != nil {
		return err
	}

	info, err := os.Stat(entry.SourcePath)
	if err != nil {
		return err
	}

	var tempPath string
	if info.IsDir() {
		tempPath, err = os.MkdirTemp(filepath.Dir(entry.DestPath), "."+filepath.Base(entry.DestPath)+".hazmat-*")
		if err != nil {
			return err
		}
		if err := copyHarnessAssetDirStrict(entry.SourcePath, tempPath); err != nil {
			_ = os.RemoveAll(tempPath)
			return err
		}
	} else {
		tempFile, err := os.CreateTemp(filepath.Dir(entry.DestPath), "."+filepath.Base(entry.DestPath)+".hazmat-*")
		if err != nil {
			return err
		}
		tempPath = tempFile.Name()
		if err := tempFile.Close(); err != nil {
			_ = os.Remove(tempPath)
			return err
		}
		if err := copyHarnessAssetFileStrict(entry.SourcePath, tempPath); err != nil {
			_ = os.Remove(tempPath)
			return err
		}
	}

	if err := replaceHarnessAssetPath(tempPath, entry.DestPath); err != nil {
		if info.IsDir() {
			_ = os.RemoveAll(tempPath)
		} else {
			_ = os.Remove(tempPath)
		}
		return err
	}
	return nil
}

func installHarnessAssetEntryAsAgent(entry harnessAssetDesiredEntry) error {
	if err := validateHarnessAssetSpecDestPath(entry.Spec, entry.DestPath); err != nil {
		return err
	}
	if err := harnessAssetAgentEnsureDir(filepath.Dir(entry.DestPath), 0o2770); err != nil {
		return err
	}

	info, err := os.Stat(entry.SourcePath)
	if err != nil {
		return err
	}

	tempPath := filepath.Join(
		filepath.Dir(entry.DestPath),
		fmt.Sprintf(".%s.hazmat-%d-%d", filepath.Base(entry.DestPath), os.Getpid(), time.Now().UnixNano()),
	)
	if info.IsDir() {
		if err := copyHarnessAssetDirStrictAsAgent(entry.SourcePath, tempPath); err != nil {
			_ = harnessAssetAgentRemoveAll(tempPath)
			return err
		}
	} else {
		if err := copyHarnessAssetFileStrictAsAgent(entry.SourcePath, tempPath); err != nil {
			_ = harnessAssetAgentRemoveAll(tempPath)
			return err
		}
	}

	if err := replaceHarnessAssetPathAsAgent(tempPath, entry.DestPath); err != nil {
		_ = harnessAssetAgentRemoveAll(tempPath)
		return err
	}
	return nil
}

func copyHarnessAssetDirStrict(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("nested symlink %s is not supported", src)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, 0o2770); err != nil {
		return err
	}
	if err := os.Chmod(dst, 0o2770); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childSrc := filepath.Join(src, entry.Name())
		childInfo, err := os.Lstat(childSrc)
		if err != nil {
			return err
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("nested symlink %s is not supported", childSrc)
		}

		childDst := filepath.Join(dst, entry.Name())
		if childInfo.IsDir() {
			if err := copyHarnessAssetDirStrict(childSrc, childDst); err != nil {
				return err
			}
			continue
		}
		if !childInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported nested source type %s at %s", childInfo.Mode().String(), childSrc)
		}
		if err := copyHarnessAssetFileStrict(childSrc, childDst); err != nil {
			return err
		}
	}
	return nil
}

func copyHarnessAssetDirStrictAsAgent(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("nested symlink %s is not supported", src)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := harnessAssetAgentEnsureDir(dst, 0o2770); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childSrc := filepath.Join(src, entry.Name())
		childInfo, err := os.Lstat(childSrc)
		if err != nil {
			return err
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("nested symlink %s is not supported", childSrc)
		}

		childDst := filepath.Join(dst, entry.Name())
		if childInfo.IsDir() {
			if err := copyHarnessAssetDirStrictAsAgent(childSrc, childDst); err != nil {
				return err
			}
			continue
		}
		if !childInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported nested source type %s at %s", childInfo.Mode().String(), childSrc)
		}
		if err := copyHarnessAssetFileStrictAsAgent(childSrc, childDst); err != nil {
			return err
		}
	}
	return nil
}

func copyHarnessAssetFileStrict(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("nested symlink %s is not supported", src)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported source type %s at %s", info.Mode().String(), src)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o2770); err != nil {
		return err
	}
	mode := portableFileMode(info.Mode())
	if err := os.WriteFile(dst, raw, mode); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func copyHarnessAssetFileStrictAsAgent(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("nested symlink %s is not supported", src)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported source type %s at %s", info.Mode().String(), src)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := harnessAssetAgentEnsureDir(filepath.Dir(dst), 0o2770); err != nil {
		return err
	}
	return harnessAssetAgentWriteFile(dst, raw, portableFileMode(info.Mode()))
}

func replaceHarnessAssetPath(tempPath, destPath string) error {
	if _, err := os.Lstat(destPath); os.IsNotExist(err) {
		return os.Rename(tempPath, destPath)
	} else if err != nil {
		return err
	}

	backupPath := filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".hazmat-old-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.Rename(destPath, backupPath); err != nil {
		return err
	}
	if err := os.Rename(tempPath, destPath); err != nil {
		_ = os.Rename(backupPath, destPath)
		return err
	}
	return os.RemoveAll(backupPath)
}

func replaceHarnessAssetPathAsAgent(tempPath, destPath string) error {
	exists, err := harnessAssetAgentPathExists(destPath)
	if err != nil {
		return err
	}
	if !exists {
		return harnessAssetAgentRename(tempPath, destPath)
	}

	backupPath := filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".hazmat-old-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := harnessAssetAgentRename(destPath, backupPath); err != nil {
		return err
	}
	if err := harnessAssetAgentRename(tempPath, destPath); err != nil {
		_ = harnessAssetAgentRename(backupPath, destPath)
		return err
	}
	return harnessAssetAgentRemoveAll(backupPath)
}

func removeHarnessAssetPath(path string) error {
	if err := validateHarnessAssetDestPath(path); err != nil {
		return err
	}
	if harnessAssetUseAgentBackend(path) {
		return harnessAssetAgentRemoveAll(path)
	}
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func defaultHarnessAssetUseAgentBackend(path string) bool {
	if filepath.Clean(harnessAssetAgentHome) != filepath.Clean(agentHome) {
		return false
	}
	clean := filepath.Clean(path)
	return clean == filepath.Clean(agentHome) || isWithinDir(agentHome, clean)
}

func defaultHarnessAssetAgentEnsureDir(path string, mode os.FileMode) error {
	if err := agentMkdirAll(path); err != nil {
		parent := filepath.Dir(path)
		if parent == path {
			return err
		}
		if repairErr := harnessAssetAgentRepairDir(parent, 0o2770); repairErr != nil {
			return fmt.Errorf("%w; repair parent %s: %w", err, parent, repairErr)
		}
		if retryErr := agentMkdirAll(path); retryErr != nil {
			return retryErr
		}
	}

	writable, err := harnessAssetAgentDirWritable(path)
	if err != nil {
		return err
	}
	if writable {
		return nil
	}

	if err := harnessAssetAgentRepairDir(path, mode); err != nil {
		return err
	}
	writable, err = harnessAssetAgentDirWritable(path)
	if err != nil {
		return err
	}
	if !writable {
		return fmt.Errorf("%s is not writable by %s after repair", path, agentUser)
	}
	return nil
}

func defaultHarnessAssetAgentDirWritable(path string) (bool, error) {
	const script = `if [ -d "$1" ] && [ -w "$1" ]; then
  printf yes
else
  printf no
fi`
	out, err := asAgentOutput("/bin/sh", "-c", script, "hazmat-harness-asset-dir-writable", path)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(out) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected agent directory writable probe output for %s: %q", path, out)
	}
}

func defaultHarnessAssetAgentRepairDir(path string, mode os.FileMode) error {
	if err := validateHarnessAssetDestPath(path); err != nil {
		return err
	}
	isLink, err := agentPathIsSymlink(path)
	if err != nil {
		return err
	}
	if isLink {
		return fmt.Errorf("refusing to repair symlinked harness asset directory %s", path)
	}
	isDir, err := agentPathIsDir(path)
	if err != nil {
		return err
	}
	if !isDir {
		return fmt.Errorf("cannot repair non-directory harness asset path %s", path)
	}
	if err := sudoNoPrompt("chown", agentUser+":"+sharedGroup, path); err != nil {
		return fmt.Errorf("set owner on %s: %w", path, err)
	}
	if err := sudoNoPrompt("chmod", fileModeString(mode), path); err != nil {
		return fmt.Errorf("set mode on %s: %w", path, err)
	}
	return nil
}

func defaultHarnessAssetAgentRename(src, dst string) error {
	if err := asAgentQuiet("/bin/mv", "-f", src, dst); err != nil {
		return fmt.Errorf("rename %s to %s: %w", src, dst, err)
	}
	return nil
}

func defaultHarnessAssetAgentRemoveAll(path string) error {
	if err := asAgentQuiet("/bin/rm", "-rf", path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
