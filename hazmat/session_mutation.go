package hazmat

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"hazmat/sessioncontract"
)

const sessionMutationProofScopeTLAModel = "TLA+ model + tests/docs"
const sessionMutationProofScopeTestsDocs = "tests/docs"

type sessionMutation = sessioncontract.HostMutation

type sessionMutationExecution struct {
	AppliedMessage string
	Warning        string
}

type plannedSessionMutation struct {
	Metadata sessionMutation
	Apply    func() (sessionMutationExecution, error)
}

type sessionMutationPlan struct {
	Mutations []plannedSessionMutation
}

type nativeSessionMutationPlanOptions struct {
	SkipGitSafeDirectoryPlanning bool
}

var nativeSessionMutationPlanProfileWriter = func() io.Writer { return os.Stderr }

type nativeSessionMutationPlanProfile struct {
	enabled bool
	w       io.Writer
	spans   []sessionPreparationSpan
}

func newNativeSessionMutationPlanProfile() *nativeSessionMutationPlanProfile {
	return &nativeSessionMutationPlanProfile{
		enabled: sessionPreparationProfileEnabled(),
		w:       nativeSessionMutationPlanProfileWriter(),
	}
}

func (p *nativeSessionMutationPlanProfile) Record(label string, start time.Time) {
	if p == nil || !p.enabled || p.w == nil {
		return
	}
	duration := time.Since(start)
	if duration < 0 {
		duration = 0
	}
	p.spans = append(p.spans, sessionPreparationSpan{
		Label:    label,
		Duration: duration,
	})
}

func (p *nativeSessionMutationPlanProfile) Done() {
	if p == nil || !p.enabled || p.w == nil || len(p.spans) == 0 {
		return
	}
	fmt.Fprintln(p.w, "hazmat: native host repair planning profile:")
	for _, span := range p.spans {
		fmt.Fprintf(p.w, "  %s: %.3fs\n", span.Label, span.Duration.Seconds())
	}
}

func mergeSessionMutationPlans(plans ...sessionMutationPlan) sessionMutationPlan {
	merged := sessionMutationPlan{}
	seen := make(map[string]struct{})
	for _, plan := range plans {
		for _, mutation := range plan.Mutations {
			key := mutation.Metadata.Summary + "\x00" + mutation.Metadata.Detail
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			merged.Mutations = append(merged.Mutations, mutation)
		}
	}
	return merged
}

func (p sessionMutationPlan) Describe() []sessionMutation {
	if len(p.Mutations) == 0 {
		return nil
	}
	described := make([]sessionMutation, 0, len(p.Mutations))
	for _, mutation := range p.Mutations {
		described = append(described, mutation.Metadata)
	}
	return described
}

func buildNativeSessionMutationPlan(cfg sessionConfig) sessionMutationPlan {
	return buildNativeSessionMutationPlanWithOptions(cfg, nativeSessionMutationPlanOptions{})
}

func buildNativeSessionMutationPlanWithOptions(cfg sessionConfig, opts nativeSessionMutationPlanOptions) sessionMutationPlan {
	var plan sessionMutationPlan
	profile := newNativeSessionMutationPlanProfile()
	defer profile.Done()

	helperPath := launchHelperPath()
	exposedDirs := append(append([]string{}, cfg.ReadDirs...), cfg.WriteDirs...)
	// Include project dir's parent so the full path from home to the project
	// is traversable — not just paths to extra read/write dirs.
	if parent := filepath.Dir(cfg.ProjectDir); parent != cfg.ProjectDir {
		exposedDirs = append(exposedDirs, parent)
	}

	start := time.Now()
	aclRows := readStartupACLsForPaths(nativeSessionACLProbePaths(cfg.ProjectDir, helperPath, exposedDirs))
	profile.Record("host ACL snapshot read", start)
	traversePermissions := newAgentTraversePermissionCacheWithACLRows(aclRows)

	start = time.Now()
	needsProjectACLRepair := projectNeedsACLRepairWithACLRows(cfg.ProjectDir, aclRows[cfg.ProjectDir])
	profile.Record("project ACL repair detection", start)
	if needsProjectACLRepair {
		projectDir := cfg.ProjectDir
		plan.Mutations = append(plan.Mutations, plannedSessionMutation{
			Metadata: sessionMutation{
				Summary:     "project ACL repair",
				Detail:      fmt.Sprintf("may add bounded collaborative ACLs on %s and shallow existing paths so startup is not proportional to repository size", projectDir),
				Persistence: "persistent in project",
				ProofScope:  sessionMutationProofScopeTLAModel,
			},
			Apply: func() (sessionMutationExecution, error) {
				outcome, err := ensureProjectWritable(projectDir)
				if err != nil {
					return sessionMutationExecution{
						Warning: fmt.Sprintf("could not fully set project ACL: %v", err),
					}, nil
				}
				if outcome.Fixed {
					result := sessionMutationExecution{
						AppliedMessage: "  Fixed bounded project permissions for agent access",
					}
					if outcome.Truncated {
						result.Warning = "project ACL startup repair hit its bounded scan cap; full historical backfill was deferred"
					}
					return result, nil
				}
				return sessionMutationExecution{}, nil
			},
		})
	}

	if helperPath != "" {
		start := time.Now()
		pending := pendingLaunchHelperTraverseTargetsWithProbe(helperPath, traversePermissions.Allows)
		profile.Record("launch-helper traverse detection", start)
		if len(pending) > 0 {
			pendingCount := len(pending)
			plan.Mutations = append(plan.Mutations, plannedSessionMutation{
				Metadata: sessionMutation{
					Summary:     "launch-helper traverse ACL repair",
					Detail:      fmt.Sprintf("may add traverse ACLs on %d host-home path(s) so the agent can execute the user-local launch helper at %s", pendingCount, helperPath),
					Persistence: "persistent outside project",
					ProofScope:  sessionMutationProofScopeTLAModel,
				},
				Apply: func() (sessionMutationExecution, error) {
					fixed, failures := ensureAgentCanTraverseLaunchHelperPath(helperPath)
					if len(failures) > 0 {
						return sessionMutationExecution{
							Warning: fmt.Sprintf("could not fully prepare the launch helper path: %s", failures[0]),
						}, nil
					}
					if fixed {
						return sessionMutationExecution{
							AppliedMessage: "  Fixed launch-helper traversal for agent access",
						}, nil
					}
					return sessionMutationExecution{}, nil
				},
			})
		}
	}

	start = time.Now()
	pendingTraverse := pendingAgentTraverseTargetsWithProbe(cfg.ProjectDir, exposedDirs, traversePermissions.Allows)
	profile.Record("exposed-directory traverse detection", start)
	if len(pendingTraverse) > 0 {
		projectDir := cfg.ProjectDir
		pendingCount := len(pendingTraverse)
		plan.Mutations = append(plan.Mutations, plannedSessionMutation{
			Metadata: sessionMutation{
				Summary:     "exposed-directory traverse ACL repair",
				Detail:      fmt.Sprintf("may add traverse ACLs on %d host-home ancestor path(s) outside %s so the agent can reach exposed directories", pendingCount, projectDir),
				Persistence: "persistent outside project",
				ProofScope:  sessionMutationProofScopeTLAModel,
			},
			Apply: func() (sessionMutationExecution, error) {
				fixed, failures := ensureAgentCanTraverseExposedDirs(projectDir, exposedDirs)
				if len(failures) > 0 {
					return sessionMutationExecution{
						Warning: fmt.Sprintf("could not fully prepare exposed directories: %s", failures[0]),
					}, nil
				}
				if fixed {
					return sessionMutationExecution{
						AppliedMessage: "  Fixed exposed directory traversal for agent access",
					}, nil
				}
				return sessionMutationExecution{}, nil
			},
		})
	}

	gitDir := gitMetadataDir(cfg.ProjectDir)
	start = time.Now()
	gitProblems := []string(nil)
	if gitDir != "" {
		gitProblems = collectGitPermissionProblems(gitDir)
	}
	profile.Record("git metadata permission detection", start)
	if gitDir != "" && len(gitProblems) > 0 {
		projectDir := cfg.ProjectDir
		plan.Mutations = append(plan.Mutations, plannedSessionMutation{
			Metadata: sessionMutation{
				Summary:     "git metadata ACL repair",
				Detail:      fmt.Sprintf("may add collaborative ACLs under %s before launch if current metadata permissions are broken", gitDir),
				Persistence: "persistent in project",
				ProofScope:  sessionMutationProofScopeTLAModel,
			},
			Apply: func() (sessionMutationExecution, error) {
				fixed, err := ensureGitMetadataHealthy(projectDir)
				if err != nil {
					return sessionMutationExecution{}, err
				}
				if fixed {
					return sessionMutationExecution{
						AppliedMessage: "  Fixed Git metadata permissions for collaborative access",
					}, nil
				}
				return sessionMutationExecution{}, nil
			},
		})
	}

	start = time.Now()
	if opts.SkipGitSafeDirectoryPlanning {
		profile.Record("git safe.directory planning (skipped)", start)
	} else {
		repoDir := plannedProjectGitSafeDirectory(cfg.ProjectDir)
		profile.Record("git safe.directory planning", start)
		if repoDir != "" {
			projectDir := cfg.ProjectDir
			plan.Mutations = append(plan.Mutations, plannedSessionMutation{
				Metadata: sessionMutation{
					Summary:     "git safe.directory trust",
					Detail:      fmt.Sprintf("may add %s to the agent user's Git safe.directory list so agent-side tools can read repository metadata", repoDir),
					Persistence: "persistent in agent home",
					ProofScope:  sessionMutationProofScopeTestsDocs,
				},
				Apply: func() (sessionMutationExecution, error) {
					fixed, err := ensureAgentGitSafeDirectory(projectDir)
					if err != nil {
						return sessionMutationExecution{}, err
					}
					if fixed {
						return sessionMutationExecution{
							AppliedMessage: "  Trusted project repo for agent-side Git metadata access",
						}, nil
					}
					return sessionMutationExecution{}, nil
				},
			})
		}
	}

	return plan
}

func nativeSessionACLProbePaths(projectDir, helperPath string, exposedDirs []string) []string {
	var paths []string
	paths = append(paths, projectDir)
	paths = append(paths, launchHelperTraverseCandidatePaths(helperPath)...)
	paths = append(paths, agentTraverseCandidatePaths(projectDir, exposedDirs)...)
	return paths
}

var executeSessionMutationPlan = defaultExecuteSessionMutationPlan

func defaultExecuteSessionMutationPlan(plan sessionMutationPlan) error {
	return executeSessionMutationPlanToWriter(os.Stderr, plan)
}

func executeSessionMutationPlanToWriter(w io.Writer, plan sessionMutationPlan) error {
	for _, mutation := range plan.Mutations {
		start := time.Now()
		fmt.Fprintf(w, "  Running %s...\n", mutation.Metadata.Summary)
		result, err := mutation.Apply()
		if err != nil {
			fmt.Fprintf(w, "  Failed %s after %.1fs\n", mutation.Metadata.Summary, time.Since(start).Seconds())
			return err
		}
		if result.Warning != "" {
			fmt.Fprintf(w, "  Warning: %s\n", result.Warning)
		}
		if result.AppliedMessage != "" {
			fmt.Fprintf(w, "%s (%.1fs)\n", result.AppliedMessage, time.Since(start).Seconds())
			continue
		}
		fmt.Fprintf(w, "  Finished %s (%.1fs)\n", mutation.Metadata.Summary, time.Since(start).Seconds())
	}
	return nil
}

func sessionMutationList(mutations []sessionMutation) string {
	if len(mutations) == 0 {
		return "none"
	}
	summaries := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		summaries = append(summaries, mutation.Summary)
	}
	return sessionContractList(summaries)
}

func renderSessionMutationDetails(mutations []sessionMutation) string {
	if len(mutations) == 0 {
		return ""
	}

	var b bytes.Buffer
	fmt.Fprintln(&b, "hazmat: planned host changes")
	for _, mutation := range mutations {
		fmt.Fprintf(&b, "  - %s: %s (%s; proof scope: %s)\n",
			mutation.Summary,
			mutation.Detail,
			mutation.Persistence,
			mutation.ProofScope,
		)
	}
	b.WriteByte('\n')
	return b.String()
}
