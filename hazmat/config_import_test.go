package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClaudeImportEnv(t *testing.T) claudeImportEnv {
	t.Helper()

	hostHome := filepath.Join(t.TempDir(), "host")
	agentHome := filepath.Join(t.TempDir(), "agent")
	for _, dir := range []string{
		hostHome,
		agentHome,
		filepath.Join(hostHome, ".claude"),
		filepath.Join(agentHome, ".claude"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	return claudeImportEnv{
		hostHome:  hostHome,
		agentHome: agentHome,
	}
}

func testOpenCodeImportEnv(t *testing.T) opencodeImportEnv {
	t.Helper()

	hostHome := filepath.Join(t.TempDir(), "host")
	agentHome := filepath.Join(t.TempDir(), "agent")
	for _, dir := range []string{
		hostHome,
		agentHome,
		filepath.Join(hostHome, ".config", "opencode"),
		filepath.Join(agentHome, ".config", "opencode"),
		filepath.Join(hostHome, ".local", "share", "opencode"),
		filepath.Join(agentHome, ".local", "share", "opencode"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	return opencodeImportEnv{
		hostHome:  hostHome,
		agentHome: agentHome,
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScanClaudeImportPlanOnlyIncludesPortableBasics(t *testing.T) {
	env := testClaudeImportEnv(t)

	writeTestFile(t, env.hostClaudeStatePath(), `{
  "oauthAccount": {"emailAddress": "denis@example.com"},
  "userID": "u-123",
  "mcpServers": {"github": {"type": "stdio"}}
}`)
	writeTestFile(t, env.hostGitConfigPath(), "[user]\n\tname = Denis\n\temail = denis@example.com\n")

	commandTarget := filepath.Join(t.TempDir(), "command.md")
	writeTestFile(t, commandTarget, "# create prd\n")
	if err := os.MkdirAll(env.hostCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(commandTarget, filepath.Join(env.hostCommandsDir(), "create-prd.md")); err != nil {
		t.Fatal(err)
	}

	skillSource := filepath.Join(t.TempDir(), "skill-source")
	writeTestFile(t, filepath.Join(skillSource, "SKILL.md"), "# Skill\n")
	if err := os.MkdirAll(env.hostSkillsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillSource, filepath.Join(env.hostSkillsDir(), "brainstorming")); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(env.hostClaudeDir(), "settings.json"), `{"hooks": {}}`)

	plan, err := scanClaudeImportPlan(env, nil)
	if err != nil {
		t.Fatalf("scanClaudeImportPlan: %v", err)
	}

	if !plan.hasCategory("sign-in") {
		t.Fatal("expected sign-in item in import plan")
	}
	if !plan.hasCategory("git identity") {
		t.Fatal("expected git identity item in import plan")
	}
	if got := plan.countCategory("command"); got != 1 {
		t.Fatalf("command count = %d, want 1", got)
	}
	if got := plan.countCategory("skill"); got != 1 {
		t.Fatalf("skill count = %d, want 1", got)
	}
	if len(plan.Skips) != 0 {
		t.Fatalf("unexpected skips: %+v", plan.Skips)
	}

	for _, item := range plan.Items {
		if item.Name == "settings.json" {
			t.Fatal("settings.json should not be part of portable import scope")
		}
	}
}

func TestApplyClaudeImportPlanCopiesPortableContentAndMergesState(t *testing.T) {
	env := testClaudeImportEnv(t)

	writeTestFile(t, env.hostClaudeStatePath(), `{
  "oauthAccount": {"emailAddress": "denis@example.com"},
  "userID": "u-123",
  "mcpServers": {"github": {"type": "stdio"}}
}`)
	writeTestFile(t, env.agentClaudeStatePath(), `{
  "projects": {"hazmat": true},
  "anonymousId": "anon-1"
}`)
	writeTestFile(t, env.hostGitConfigPath(), "[user]\n\tname = Denis\n\temail = denis@example.com\n")

	commandTarget := filepath.Join(t.TempDir(), "command.md")
	writeTestFile(t, commandTarget, "# create prd\n")
	if err := os.MkdirAll(env.hostCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(commandTarget, filepath.Join(env.hostCommandsDir(), "create-prd.md")); err != nil {
		t.Fatal(err)
	}

	skillSource := filepath.Join(t.TempDir(), "skill-source")
	writeTestFile(t, filepath.Join(skillSource, "SKILL.md"), "# Skill\n")
	writeTestFile(t, filepath.Join(skillSource, "docs", "guide.md"), "guide\n")
	nestedTarget := filepath.Join(t.TempDir(), "nested-target.md")
	writeTestFile(t, nestedTarget, "nested link\n")
	if err := os.Symlink(nestedTarget, filepath.Join(skillSource, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.hostSkillsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillSource, filepath.Join(env.hostSkillsDir(), "brainstorming")); err != nil {
		t.Fatal(err)
	}

	plan, err := scanClaudeImportPlan(env, nil)
	if err != nil {
		t.Fatalf("scanClaudeImportPlan: %v", err)
	}
	if err := plan.resolveConflicts(claudeConflictOverwrite); err != nil {
		t.Fatalf("resolveConflicts: %v", err)
	}

	if _, err := applyClaudeImportPlan(plan, env, nil); err != nil {
		t.Fatalf("applyClaudeImportPlan: %v", err)
	}

	gitConfigRaw, err := os.ReadFile(env.agentGitConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	gitConfig := string(gitConfigRaw)
	if !containsAll(gitConfig, "name = Denis", "email = denis@example.com") {
		t.Fatalf("agent gitconfig missing imported identity:\n%s", gitConfig)
	}

	stateRaw, err := os.ReadFile(env.agentClaudeStatePath())
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		t.Fatal(err)
	}
	if _, ok := state["oauthAccount"]; !ok {
		t.Fatal("expected oauthAccount to be imported")
	}
	if state["userID"] != "u-123" {
		t.Fatalf("userID = %v, want u-123", state["userID"])
	}
	if _, ok := state["projects"]; !ok {
		t.Fatal("expected existing agent-only projects state to be preserved")
	}
	if _, ok := state["mcpServers"]; ok {
		t.Fatal("mcpServers should not be imported from host Claude state")
	}

	commandDest := filepath.Join(env.agentCommandsDir(), "create-prd.md")
	commandInfo, err := os.Lstat(commandDest)
	if err != nil {
		t.Fatal(err)
	}
	if commandInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected imported command to be copied as a regular file")
	}

	skillDest := filepath.Join(env.agentSkillsDir(), "brainstorming")
	if _, err := os.Stat(filepath.Join(skillDest, "SKILL.md")); err != nil {
		t.Fatalf("expected copied skill file: %v", err)
	}
	nestedCopied := filepath.Join(skillDest, "linked.md")
	nestedInfo, err := os.Lstat(nestedCopied)
	if err != nil {
		t.Fatal(err)
	}
	if nestedInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected nested symlink target to be copied as a regular file")
	}
}

func TestClaudeImportConflictPolicySkipKeepsExistingContent(t *testing.T) {
	env := testClaudeImportEnv(t)

	if err := os.MkdirAll(env.hostCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.agentCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	hostCommand := filepath.Join(t.TempDir(), "host-command.md")
	writeTestFile(t, hostCommand, "host version\n")
	if err := os.Symlink(hostCommand, filepath.Join(env.hostCommandsDir(), "create-prd.md")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(env.agentCommandsDir(), "create-prd.md"), "agent version\n")

	plan, err := scanClaudeImportPlan(env, nil)
	if err != nil {
		t.Fatalf("scanClaudeImportPlan: %v", err)
	}
	if got := plan.conflictCount(); got != 1 {
		t.Fatalf("conflictCount = %d, want 1", got)
	}
	if err := plan.resolveConflicts(claudeConflictSkip); err != nil {
		t.Fatalf("resolveConflicts: %v", err)
	}
	if _, err := applyClaudeImportPlan(plan, env, nil); err != nil {
		t.Fatalf("applyClaudeImportPlan: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(env.agentCommandsDir(), "create-prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "agent version\n" {
		t.Fatalf("agent command = %q, want existing content to be preserved", raw)
	}
}

func TestScanClaudeImportPlanSkipsBrokenPortableEntries(t *testing.T) {
	env := testClaudeImportEnv(t)

	if err := os.MkdirAll(env.hostCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing.md"), filepath.Join(env.hostCommandsDir(), "broken.md")); err != nil {
		t.Fatal(err)
	}

	plan, err := scanClaudeImportPlan(env, nil)
	if err != nil {
		t.Fatalf("scanClaudeImportPlan: %v", err)
	}
	if len(plan.Skips) != 1 {
		t.Fatalf("skip count = %d, want 1", len(plan.Skips))
	}
	if plan.Skips[0].Name != "broken.md" {
		t.Fatalf("unexpected skipped entry: %+v", plan.Skips[0])
	}
}

func TestResolveConflictsFailsWithoutExplicitPolicy(t *testing.T) {
	env := testClaudeImportEnv(t)

	if err := os.MkdirAll(env.hostCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.agentCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	hostCommand := filepath.Join(t.TempDir(), "host-command.md")
	writeTestFile(t, hostCommand, "host version\n")
	if err := os.Symlink(hostCommand, filepath.Join(env.hostCommandsDir(), "create-prd.md")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(env.agentCommandsDir(), "create-prd.md"), "agent version\n")

	plan, err := scanClaudeImportPlan(env, nil)
	if err != nil {
		t.Fatalf("scanClaudeImportPlan: %v", err)
	}
	if err := plan.resolveConflicts(claudeConflictFail); err == nil {
		t.Fatal("expected explicit conflict policy to be required")
	}
}

func TestScanOpenCodeImportPlanOnlyIncludesPortableBasics(t *testing.T) {
	env := testOpenCodeImportEnv(t)

	writeTestFile(t, env.hostAuthFile(), `{"provider":"anthropic","token":"abc123"}`)
	writeTestFile(t, env.hostGitConfigPath(), "[user]\n\tname = Denis\n\temail = denis@example.com\n")

	commandTarget := filepath.Join(t.TempDir(), "command.md")
	writeTestFile(t, commandTarget, "# ship\n")
	if err := os.MkdirAll(env.hostCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(commandTarget, filepath.Join(env.hostCommandsDir(), "ship.md")); err != nil {
		t.Fatal(err)
	}

	agentSource := filepath.Join(t.TempDir(), "agent-source")
	writeTestFile(t, filepath.Join(agentSource, "AGENT.md"), "# Agent\n")
	if err := os.MkdirAll(env.hostAgentsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(agentSource, filepath.Join(env.hostAgentsDir(), "reviewer")); err != nil {
		t.Fatal(err)
	}

	skillSource := filepath.Join(t.TempDir(), "skill-source")
	writeTestFile(t, filepath.Join(skillSource, "SKILL.md"), "# Skill\n")
	if err := os.MkdirAll(env.hostSkillsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillSource, filepath.Join(env.hostSkillsDir(), "brainstorming")); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, env.hostConfigPath(), `{"model":"anthropic/claude-sonnet-4-5"}`)

	plan, err := scanOpenCodeImportPlan(env, nil)
	if err != nil {
		t.Fatalf("scanOpenCodeImportPlan: %v", err)
	}

	if !plan.hasCategory("sign-in") {
		t.Fatal("expected sign-in item in import plan")
	}
	if !plan.hasCategory("git identity") {
		t.Fatal("expected git identity item in import plan")
	}
	if got := plan.countCategory("command"); got != 1 {
		t.Fatalf("command count = %d, want 1", got)
	}
	if got := plan.countCategory("agent"); got != 1 {
		t.Fatalf("agent count = %d, want 1", got)
	}
	if got := plan.countCategory("skill"); got != 1 {
		t.Fatalf("skill count = %d, want 1", got)
	}

	for _, item := range plan.Items {
		if item.Name == "opencode.json" {
			t.Fatal("opencode.json should not be part of portable import scope")
		}
	}
}

func TestApplyOpenCodeImportPlanCopiesPortableContent(t *testing.T) {
	env := testOpenCodeImportEnv(t)

	writeTestFile(t, env.hostAuthFile(), `{"provider":"anthropic","token":"abc123"}`)
	writeTestFile(t, env.hostGitConfigPath(), "[user]\n\tname = Denis\n\temail = denis@example.com\n")

	commandTarget := filepath.Join(t.TempDir(), "command.md")
	writeTestFile(t, commandTarget, "ship it\n")
	if err := os.MkdirAll(env.hostCommandsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(commandTarget, filepath.Join(env.hostCommandsDir(), "ship.md")); err != nil {
		t.Fatal(err)
	}

	agentSource := filepath.Join(t.TempDir(), "agent-source")
	writeTestFile(t, filepath.Join(agentSource, "AGENT.md"), "# Agent\n")
	if err := os.MkdirAll(env.hostAgentsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(agentSource, filepath.Join(env.hostAgentsDir(), "reviewer")); err != nil {
		t.Fatal(err)
	}

	skillSource := filepath.Join(t.TempDir(), "skill-source")
	writeTestFile(t, filepath.Join(skillSource, "SKILL.md"), "# Skill\n")
	if err := os.MkdirAll(env.hostSkillsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillSource, filepath.Join(env.hostSkillsDir(), "brainstorming")); err != nil {
		t.Fatal(err)
	}

	plan, err := scanOpenCodeImportPlan(env, nil)
	if err != nil {
		t.Fatalf("scanOpenCodeImportPlan: %v", err)
	}
	if err := plan.resolveConflicts(claudeConflictOverwrite); err != nil {
		t.Fatalf("resolveConflicts: %v", err)
	}
	if _, err := applyOpenCodeImportPlan(plan, env, nil); err != nil {
		t.Fatalf("applyOpenCodeImportPlan: %v", err)
	}

	authRaw, err := os.ReadFile(env.agentAuthFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(authRaw) != `{"provider":"anthropic","token":"abc123"}` {
		t.Fatalf("agent auth file = %q", authRaw)
	}

	gitConfigRaw, err := os.ReadFile(env.agentGitConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	gitConfig := string(gitConfigRaw)
	if !containsAll(gitConfig, "name = Denis", "email = denis@example.com") {
		t.Fatalf("agent gitconfig missing imported identity:\n%s", gitConfig)
	}

	commandDest := filepath.Join(env.agentCommandsDir(), "ship.md")
	commandInfo, err := os.Lstat(commandDest)
	if err != nil {
		t.Fatal(err)
	}
	if commandInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected imported command to be copied as a regular file")
	}
	if _, err := os.Stat(filepath.Join(env.agentAgentsDir(), "reviewer", "AGENT.md")); err != nil {
		t.Fatalf("expected copied agent file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.agentSkillsDir(), "brainstorming", "SKILL.md")); err != nil {
		t.Fatalf("expected copied skill file: %v", err)
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
