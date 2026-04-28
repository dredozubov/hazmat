package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type repoSetupPromptAction string

const (
	repoSetupPromptRemember      repoSetupPromptAction = "remember"
	repoSetupPromptUseOnce       repoSetupPromptAction = "use-once"
	repoSetupPromptKeepCurrent   repoSetupPromptAction = "keep-current"
	repoSetupPromptLaunchWithout repoSetupPromptAction = "launch-without"
	repoSetupPromptApproveOnce   repoSetupPromptAction = "approve-once"
	repoSetupPromptExplain       repoSetupPromptAction = "explain"
)

var promptRepoSetupSafe = defaultPromptRepoSetupSafe
var promptRepoSetupExplicit = defaultPromptRepoSetupExplicit

func shouldPromptRepoSetup() bool {
	if flagDryRun {
		return false
	}
	return flagYesAll || uiIsTerminal()
}

func finalizePreparedRepoSetup(prepared preparedSession, interactive, persist bool) (preparedSession, error) {
	if prepared.Config.RepoSetup == nil {
		return prepared, nil
	}

	state := prepared.Config.RepoSetup
	finalEffects := state.appliedSafe.union(state.appliedExplicit)
	record := state.record
	rememberedEffects := record.Remembered.normalized()
	rejectedSafe := repoSetupRejectedIDs(record.RejectedSafe)

	if interactive && shouldPromptRepoSetup() {
		if len(state.PendingExplicit) > 0 {
			action, err := promptRepoSetupExplicit(*state)
			if err != nil {
				return prepared, err
			}
			switch action {
			case repoSetupPromptRemember:
				finalEffects = finalEffects.union(state.currentExplicit)
				rememberedEffects = rememberedEffects.union(state.currentExplicit)
			case repoSetupPromptApproveOnce:
				finalEffects = finalEffects.union(state.currentExplicit)
			case repoSetupPromptLaunchWithout:
			case repoSetupPromptUseOnce:
				finalEffects = finalEffects.union(state.currentExplicit)
			case repoSetupPromptKeepCurrent, repoSetupPromptExplain:
			}
		}

		if len(state.PendingSafe) > 0 {
			action, err := promptRepoSetupSafe(*state)
			if err != nil {
				return prepared, err
			}
			switch action {
			case repoSetupPromptRemember:
				finalEffects = finalEffects.union(state.currentSafe)
				rememberedEffects = rememberedEffects.union(state.currentSafe)
				for _, effect := range state.PendingSafe {
					delete(rejectedSafe, effect.ID)
				}
			case repoSetupPromptUseOnce:
				finalEffects = finalEffects.union(state.currentSafe)
			case repoSetupPromptKeepCurrent:
				for _, effect := range state.PendingSafe {
					rejectedSafe[effect.ID] = struct{}{}
				}
			case repoSetupPromptApproveOnce:
				finalEffects = finalEffects.union(state.currentSafe)
			case repoSetupPromptLaunchWithout, repoSetupPromptExplain:
			}
		}
	}

	if interactive && !shouldPromptRepoSetup() && len(state.PendingExplicit) > 0 {
		return preparedSession{}, fmt.Errorf("additional repo setup approval required (%s); rerun interactively to approve it, or inspect with: hazmat explain -C %s",
			repoSetupEffectKindsSummary(state.PendingExplicit),
			prepared.Config.ProjectDir,
		)
	}

	if !state.currentSafe.empty() && state.currentSafe.subsetOf(finalEffects) {
		prepared.HostMutationPlan = mergeSessionMutationPlans(prepared.HostMutationPlan, state.CandidateMutationPlan)
	}
	applyRepoSetupEffects(&prepared.Config, finalEffects, *state)

	record.ProjectDir = prepared.Config.ProjectDir
	record.LastSeenHash = state.CandidateHash
	record.RejectedSafe = mapKeysSorted(rejectedSafe)
	record.Remembered = rememberedEffects.intersection(finalEffects)
	record.ApprovalHash = record.Remembered.hash()
	if persist {
		if err := saveRepoProfileRecord(record); err != nil {
			state.Notes = append(state.Notes, fmt.Sprintf("Could not save repo setup state: %v", err))
		}
	}

	state.record = record
	state = recomputeRepoSetupDisplayState(*state, finalEffects)
	prepared.Config.RepoSetup = state
	prepared.Config.PlannedHostMutations = prepared.HostMutationPlan.Describe()
	return prepared, nil
}

func recomputeRepoSetupDisplayState(state repoSetupState, final repoSetupStoredEffects) *repoSetupState {
	state.AppliedSafe = repoSetupFilterEffects(state.currentSafeEffects, final)
	state.AppliedExplicit = repoSetupFilterEffects(state.currentExplicitEffects, final)
	appliedIDs := final.ids()
	rejectedIDs := repoSetupRejectedIDs(state.record.RejectedSafe)
	for id := range appliedIDs {
		rejectedIDs[id] = struct{}{}
	}
	state.PendingSafe = repoSetupSubtractEffects(state.currentSafeEffects, rejectedIDs)
	state.PendingExplicit = repoSetupSubtractEffects(state.currentExplicitEffects, appliedIDs)
	state.appliedSafe = repoSetupStoredEffectsFromEffects(state.AppliedSafe)
	state.appliedExplicit = repoSetupStoredEffectsFromEffects(state.AppliedExplicit)
	return &state
}

func applyRepoSetupEffects(cfg *sessionConfig, effects repoSetupStoredEffects, state repoSetupState) {
	effectByID := repoSetupEffectByID(append(append([]repoSetupEffect{}, state.currentSafeEffects...), state.currentExplicitEffects...))

	if len(effects.ReadOnly) > 0 {
		var added []string
		cfg.ReadDirs, added = appendUniqueDirs(cfg.ReadDirs, effects.ReadOnly)
		cfg.AutoReadDirs, _ = appendUniqueDirs(cfg.AutoReadDirs, added)
	}
	if len(effects.Write) > 0 {
		cfg.WriteDirs, _ = appendUniqueDirs(cfg.WriteDirs, effects.Write)
	}
	if len(effects.SnapshotExcludes) > 0 {
		seen := stringSet(cfg.BackupExcludes)
		for _, exclude := range effects.SnapshotExcludes {
			if _, ok := seen[exclude]; ok {
				continue
			}
			cfg.BackupExcludes = append(cfg.BackupExcludes, exclude)
			seen[exclude] = struct{}{}
		}
	}
	if cfg.IntegrationEnv == nil {
		cfg.IntegrationEnv = make(map[string]string)
	}
	for _, key := range effects.EnvSelectors {
		id := "env:" + key
		value := ""
		if effect, ok := effectByID[id]; ok {
			value = effect.ResolvedValue
		}
		if value == "" {
			value = os.Getenv(key)
		}
		if value == "" {
			continue
		}
		cfg.IntegrationEnv[key] = value
	}
}

func repoSetupSummary(state *repoSetupState) string {
	if state == nil {
		return ""
	}
	appliedCount := len(state.AppliedSafe) + len(state.AppliedExplicit)
	pendingSafeSummary := repoSetupEffectKindsSummary(state.PendingSafe)
	pendingExplicitSummary := repoSetupEffectKindsSummary(state.PendingExplicit)
	appliedEffects := append(append([]repoSetupEffect{}, state.AppliedSafe...), state.AppliedExplicit...)
	appliedSummary := repoSetupEffectKindsSummary(appliedEffects)
	appliedStatus := ""
	if appliedCount > 0 {
		appliedStored := repoSetupStoredEffectsFromEffects(appliedEffects)
		if remembered := state.record.Remembered.normalized(); !remembered.empty() && appliedStored.subsetOf(remembered) {
			appliedStatus = "remembered"
		} else {
			appliedStatus = "active for this launch"
		}
	}

	switch {
	case appliedCount > 0 && pendingSafeSummary == "" && pendingExplicitSummary == "":
		return appliedStatus + " (" + appliedSummary + ")"
	case appliedCount > 0 && pendingSafeSummary != "" && pendingExplicitSummary == "":
		return appliedStatus + " (" + appliedSummary + "); additional repo setup available (" + pendingSafeSummary + ")"
	case appliedCount > 0 && pendingExplicitSummary != "":
		return appliedStatus + " (" + appliedSummary + "); additional approval required (" + pendingExplicitSummary + ")"
	case appliedCount == 0 && pendingExplicitSummary != "":
		return "approval required (" + pendingExplicitSummary + ")"
	case appliedCount == 0 && pendingSafeSummary != "":
		return "available (" + pendingSafeSummary + ")"
	default:
		return ""
	}
}

func renderRepoSetupDetails(state *repoSetupState) string {
	if state == nil {
		return ""
	}
	var b strings.Builder
	if summary := repoSetupSummary(state); summary != "" {
		fmt.Fprintf(&b, "hazmat: repo setup %s\n", summary)
	}
	if len(state.AppliedSafe)+len(state.AppliedExplicit) > 0 {
		fmt.Fprintln(&b, "  Applied:")
		for _, effect := range append(append([]repoSetupEffect{}, state.AppliedSafe...), state.AppliedExplicit...) {
			fmt.Fprintf(&b, "    - %s: %s (%s)\n", repoSetupEffectLabel(effect.Kind), effect.Value, strings.Join(effect.Sources, ", "))
		}
	}
	if len(state.PendingExplicit) > 0 {
		fmt.Fprintln(&b, "  Approval required:")
		for _, effect := range state.PendingExplicit {
			fmt.Fprintf(&b, "    - %s: %s (%s)\n", repoSetupEffectLabel(effect.Kind), effect.Value, strings.Join(effect.Sources, ", "))
		}
	}
	if len(state.PendingSafe) > 0 {
		fmt.Fprintln(&b, "  Available:")
		for _, effect := range state.PendingSafe {
			fmt.Fprintf(&b, "    - %s: %s (%s)\n", repoSetupEffectLabel(effect.Kind), effect.Value, strings.Join(effect.Sources, ", "))
		}
	}
	for _, note := range state.Notes {
		fmt.Fprintf(&b, "  Note: %s\n", note)
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

func renderRepoSetupLaunchDetails(state *repoSetupState) string {
	if state == nil {
		return ""
	}
	var b strings.Builder
	if len(state.AppliedSafe)+len(state.AppliedExplicit) > 0 {
		fmt.Fprintf(&b, "hazmat: repo setup %s\n", repoSetupSummary(state))
	}
	if len(state.PendingExplicit) > 0 {
		fmt.Fprintln(&b, "hazmat: additional approval remains available from repo setup")
		for _, effect := range state.PendingExplicit {
			fmt.Fprintf(&b, "  - %s: %s\n", repoSetupEffectLabel(effect.Kind), effect.Value)
		}
	}
	if len(state.PendingSafe) > 0 {
		fmt.Fprintln(&b, "hazmat: additional repo setup remains available")
		for _, effect := range state.PendingSafe {
			fmt.Fprintf(&b, "  - %s: %s\n", repoSetupEffectLabel(effect.Kind), effect.Value)
		}
	}
	for _, note := range state.Notes {
		fmt.Fprintf(&b, "hazmat: note: %s\n", note)
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

func repoSetupEffectLabel(kind repoSetupEffectKind) string {
	switch kind {
	case repoSetupEffectReadOnly:
		return "read-only"
	case repoSetupEffectWrite:
		return "write"
	case repoSetupEffectSnapshotExclude:
		return "snapshot exclude"
	case repoSetupEffectEnvSelector:
		return "env selector"
	default:
		return string(kind)
	}
}

func defaultPromptRepoSetupSafe(state repoSetupState) (repoSetupPromptAction, error) {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	for {
		fmt.Fprintln(os.Stderr)
		if len(state.AppliedSafe)+len(state.AppliedExplicit) > 0 {
			fmt.Fprintln(os.Stderr, "hazmat: additional repo setup available")
			fmt.Fprintln(os.Stderr)
			if strings.HasPrefix(repoSetupSummary(&state), "remembered ") {
				fmt.Fprintln(os.Stderr, "Hazmat is already using remembered repo setup.")
			} else {
				fmt.Fprintln(os.Stderr, "Hazmat is already using repo setup for this launch.")
			}
		} else {
			fmt.Fprintln(os.Stderr, "hazmat: repo setup available")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "This repo can launch now.")
		}
		fmt.Fprintf(os.Stderr, "Additional repo setup can add: %s\n", repoSetupEffectKindsSummary(state.PendingSafe))
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "These changes do not widen write access, expose credentials, or change network policy.")
		fmt.Fprintln(os.Stderr)

		choices := []UIChoice{
			{
				Key:         string(repoSetupPromptRemember),
				Label:       "Remember for this repo",
				Description: "Apply the available repo setup now and store it in host-owned state for future launches.",
			},
			{
				Key:         string(repoSetupPromptUseOnce),
				Label:       "Use once",
				Description: "Apply the available repo setup only for this launch.",
			},
			{
				Key:         string(repoSetupPromptKeepCurrent),
				Label:       "Keep current repo setup",
				Description: "Launch without these additional changes and suppress this exact safe suggestion set until it changes.",
			},
			{
				Key:         string(repoSetupPromptExplain),
				Label:       "Explain",
				Description: "Show the exact effects and where Hazmat learned them.",
			},
		}
		if len(state.AppliedSafe)+len(state.AppliedExplicit) == 0 {
			choices[2].Label = "Launch strict"
		}
		choice, err := ui.Choose("Choose repo setup handling [1-4, Enter for default]:", choices, repoSetupSafeDefaultChoice(ui))
		if err != nil {
			return "", err
		}
		if repoSetupPromptAction(choice) == repoSetupPromptExplain {
			fmt.Fprint(os.Stderr, renderRepoSetupDetails(&state))
			continue
		}
		return repoSetupPromptAction(choice), nil
	}
}

func repoSetupSafeDefaultChoice(ui *UI) string {
	if ui != nil && ui.YesAll {
		return string(repoSetupPromptUseOnce)
	}
	return string(repoSetupPromptRemember)
}

func defaultPromptRepoSetupExplicit(state repoSetupState) (repoSetupPromptAction, error) {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	for {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "hazmat: additional approval required")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "This repo appears to need: %s\n", repoSetupEffectKindsSummary(state.PendingExplicit))
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Hazmat will not add this scope automatically.")
		fmt.Fprintln(os.Stderr)

		choice, err := ui.Choose("Choose approval handling [1-4, Enter for default]:", []UIChoice{
			{
				Key:         string(repoSetupPromptRemember),
				Label:       "Approve for this repo",
				Description: "Store this additional approval in host-owned repo setup state.",
			},
			{
				Key:         string(repoSetupPromptApproveOnce),
				Label:       "Approve once",
				Description: "Use this additional approval for this launch only.",
			},
			{
				Key:         string(repoSetupPromptLaunchWithout),
				Label:       "Launch without it",
				Description: "Continue without this extra authority.",
			},
			{
				Key:         string(repoSetupPromptExplain),
				Label:       "Explain",
				Description: "Show the exact effects and where Hazmat learned them.",
			},
		}, repoSetupExplicitDefaultChoice(ui))
		if err != nil {
			return "", err
		}
		if repoSetupPromptAction(choice) == repoSetupPromptExplain {
			fmt.Fprint(os.Stderr, renderRepoSetupDetails(&state))
			continue
		}
		return repoSetupPromptAction(choice), nil
	}
}

func repoSetupExplicitDefaultChoice(ui *UI) string {
	if ui != nil && ui.YesAll {
		return string(repoSetupPromptLaunchWithout)
	}
	return string(repoSetupPromptLaunchWithout)
}

func mapKeysSorted(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
