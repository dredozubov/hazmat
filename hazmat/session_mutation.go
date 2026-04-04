package main

import (
	"bytes"
	"fmt"
	"os"
)

const sessionMutationProofScopeTLAModel = "TLA+ model + tests/docs"
const sessionMutationProofScopeTestsDocs = "tests/docs"

type sessionMutation struct {
	Summary     string
	Detail      string
	Persistence string
	ProofScope  string
}

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
	var plan sessionMutationPlan

	if projectNeedsACLRepair(cfg.ProjectDir) {
		projectDir := cfg.ProjectDir
		plan.Mutations = append(plan.Mutations, plannedSessionMutation{
			Metadata: sessionMutation{
				Summary:     "project ACL repair",
				Detail:      fmt.Sprintf("may add collaborative ACLs under %s so the agent can edit existing files", projectDir),
				Persistence: "persistent in project",
				ProofScope:  sessionMutationProofScopeTLAModel,
			},
			Apply: func() (sessionMutationExecution, error) {
				fixed, err := ensureProjectWritable(projectDir)
				if err != nil {
					return sessionMutationExecution{
						Warning: fmt.Sprintf("could not fully set project ACL: %v", err),
					}, nil
				}
				if fixed {
					return sessionMutationExecution{
						AppliedMessage: "  Fixed project permissions for agent access",
					}, nil
				}
				return sessionMutationExecution{}, nil
			},
		})
	}

	exposedDirs := append(append([]string{}, cfg.ReadDirs...), cfg.WriteDirs...)
	if pending := pendingAgentTraverseTargets(cfg.ProjectDir, exposedDirs); len(pending) > 0 {
		projectDir := cfg.ProjectDir
		pendingCount := len(pending)
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
	if gitDir != "" && len(collectGitPermissionProblems(gitDir)) > 0 {
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

	if repoDir := plannedProjectGitSafeDirectory(cfg.ProjectDir); repoDir != "" {
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

	return plan
}

func executeSessionMutationPlan(plan sessionMutationPlan) error {
	for _, mutation := range plan.Mutations {
		result, err := mutation.Apply()
		if err != nil {
			return err
		}
		if result.Warning != "" {
			fmt.Fprintf(os.Stderr, "  Warning: %s\n", result.Warning)
		}
		if result.AppliedMessage != "" {
			fmt.Fprintln(os.Stderr, result.AppliedMessage)
		}
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
