package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	stackcheckModeDetect   = "detect"
	stackcheckModeContract = "contract"
	stackcheckModeSmoke    = "smoke"

	stackcheckStatusPass = "pass"
	stackcheckStatusFail = "fail"
)

type stackcheckOptions struct {
	ManifestPath  string
	WorkspaceRoot string
	Track         string
	Wave          int
	IDs           []string
}

type stackcheckResultSet struct {
	ManifestPath  string                 `json:"manifest_path"`
	WorkspaceRoot string                 `json:"workspace_root"`
	Mode          string                 `json:"mode"`
	Track         string                 `json:"track,omitempty"`
	Wave          int                    `json:"wave,omitempty"`
	Results       []stackcheckRepoResult `json:"results"`
}

type stackcheckRepoResult struct {
	ID              string                    `json:"id"`
	Repo            string                    `json:"repo"`
	Ref             string                    `json:"ref"`
	Wave            int                       `json:"wave"`
	Track           string                    `json:"track"`
	Status          string                    `json:"status"`
	FailureClass    string                    `json:"failure_class,omitempty"`
	Message         string                    `json:"message,omitempty"`
	RepoDir         string                    `json:"repo_dir,omitempty"`
	DetectPreview   *explainJSONPreview       `json:"detect_preview,omitempty"`
	ContractPreview *explainJSONPreview       `json:"contract_preview,omitempty"`
	Commands        []stackcheckCommandResult `json:"commands,omitempty"`
	DurationMS      int64                     `json:"duration_ms"`
}

type stackcheckCommandResult struct {
	Step       string   `json:"step"`
	Command    []string `json:"command"`
	ExitCode   int      `json:"exit_code"`
	DurationMS int64    `json:"duration_ms"`
	Stdout     string   `json:"stdout,omitempty"`
	Stderr     string   `json:"stderr,omitempty"`
}

type stackcheckCommandOutcome struct {
	stdout   string
	stderr   string
	exitCode int
	duration time.Duration
}

func newStackCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "stackcheck",
		Short:  "Internal repo-matrix validation runner",
		Hidden: true,
	}
	cmd.AddCommand(
		newStackCheckRunCmd(stackcheckModeDetect),
		newStackCheckRunCmd(stackcheckModeContract),
		newStackCheckRunCmd(stackcheckModeSmoke),
	)
	return cmd
}

func newStackCheckRunCmd(mode string) *cobra.Command {
	var opts stackcheckOptions
	cmd := &cobra.Command{
		Use:    mode,
		Short:  fmt.Sprintf("Run stack-matrix %s checks", mode),
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.ManifestPath == "" {
				opts.ManifestPath = defaultStackMatrixManifestPath()
			}
			if opts.WorkspaceRoot == "" {
				opts.WorkspaceRoot = defaultStackcheckWorkspaceRoot()
			}

			results, err := runStackCheck(mode, opts)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			if err := enc.Encode(results); err != nil {
				return err
			}

			fmt.Fprint(cmd.ErrOrStderr(), summarizeStackcheckResults(results))
			if failed := stackcheckFailureCount(results); failed > 0 {
				return fmt.Errorf("%d stackcheck repo(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ManifestPath, "manifest", defaultStackMatrixManifestPath(),
		"Path to the repo corpus manifest")
	cmd.Flags().StringVar(&opts.WorkspaceRoot, "workspace-root", defaultStackcheckWorkspaceRoot(),
		"Directory where pinned repo checkouts are stored")
	cmd.Flags().StringVar(&opts.Track, "track", stackMatrixTrackRequired,
		`Repo track to run: "required", "informational", or "all"`)
	cmd.Flags().IntVar(&opts.Wave, "wave", 0,
		"Only run repos from a specific wave (0 means all waves)")
	cmd.Flags().StringArrayVar(&opts.IDs, "id", nil,
		"Run only the named repo id(s) from the manifest")
	return cmd
}

func runStackCheck(mode string, opts stackcheckOptions) (stackcheckResultSet, error) {
	manifest, err := loadStackMatrixManifest(opts.ManifestPath)
	if err != nil {
		return stackcheckResultSet{}, err
	}

	selection := stackMatrixSelection{
		Track: opts.Track,
		Wave:  opts.Wave,
		IDs:   make(map[string]struct{}, len(opts.IDs)),
	}
	for _, id := range opts.IDs {
		selection.IDs[id] = struct{}{}
	}

	repos := selectStackMatrixRepos(manifest, selection)
	if len(repos) == 0 {
		return stackcheckResultSet{}, fmt.Errorf("no repos selected from %s", opts.ManifestPath)
	}

	selfPath, err := os.Executable()
	if err != nil {
		return stackcheckResultSet{}, fmt.Errorf("resolve current executable: %w", err)
	}
	if err := os.MkdirAll(opts.WorkspaceRoot, 0o755); err != nil {
		return stackcheckResultSet{}, fmt.Errorf("create workspace root %s: %w", opts.WorkspaceRoot, err)
	}

	resultSet := stackcheckResultSet{
		ManifestPath:  opts.ManifestPath,
		WorkspaceRoot: opts.WorkspaceRoot,
		Mode:          mode,
		Track:         opts.Track,
		Wave:          opts.Wave,
		Results:       make([]stackcheckRepoResult, 0, len(repos)),
	}

	for _, repo := range repos {
		resultSet.Results = append(resultSet.Results, runStackCheckForRepo(selfPath, opts.WorkspaceRoot, repo, mode))
	}

	return resultSet, nil
}

func runStackCheckForRepo(selfPath, workspaceRoot string, repo stackMatrixRepo, mode string) stackcheckRepoResult {
	start := time.Now()
	result := stackcheckRepoResult{
		ID:     repo.ID,
		Repo:   repo.Repo,
		Ref:    repo.Ref,
		Wave:   repo.Wave,
		Track:  repo.Track,
		Status: stackcheckStatusPass,
	}
	repoDir, err := ensureStackcheckRepoCheckout(workspaceRoot, repo)
	if err != nil {
		result.Status = stackcheckStatusFail
		result.FailureClass = "repo_setup_failure"
		result.Message = err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	result.RepoDir = repoDir

	detectPreview, cmdResult, err := runStackcheckExplain(selfPath, repoDir, nil)
	result.Commands = append(result.Commands, cmdResult)
	if err != nil {
		result.Status = stackcheckStatusFail
		result.FailureClass = "repo_setup_failure"
		result.Message = cmdResult.Stderr
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	result.DetectPreview = &detectPreview
	if failureClass, message := validateStackcheckDetect(repo, detectPreview); failureClass != "" {
		result.Status = stackcheckStatusFail
		result.FailureClass = failureClass
		result.Message = message
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	if mode == stackcheckModeDetect {
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	contractPreview, cmdResult, err := runStackcheckExplain(selfPath, repoDir, repo.Activate)
	result.Commands = append(result.Commands, cmdResult)
	if err != nil {
		result.Status = stackcheckStatusFail
		result.FailureClass = classifyStackcheckProcessFailure("contract", cmdResult)
		result.Message = stackcheckFailureMessage(cmdResult, err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	result.ContractPreview = &contractPreview
	if failureClass, message := validateStackcheckContract(repo, contractPreview); failureClass != "" {
		result.Status = stackcheckStatusFail
		result.FailureClass = failureClass
		result.Message = message
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	if mode == stackcheckModeContract {
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	containmentCheck := "id -un | grep -qx agent\n" +
		"test ! -r \"$HOME/.ssh\""
	cmdResult, err = runStackcheckExec(selfPath, repoDir, repo.Activate, containmentCheck, "containment")
	result.Commands = append(result.Commands, cmdResult)
	if err != nil {
		result.Status = stackcheckStatusFail
		result.FailureClass = "containment_failure"
		result.Message = stackcheckFailureMessage(cmdResult, err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	for _, smokeCommand := range repo.SmokeCommands {
		cmdResult, err = runStackcheckExec(selfPath, repoDir, repo.Activate, smokeCommand, "workflow")
		result.Commands = append(result.Commands, cmdResult)
		if err != nil {
			result.Status = stackcheckStatusFail
			result.FailureClass = classifyStackcheckProcessFailure("workflow", cmdResult)
			result.Message = stackcheckFailureMessage(cmdResult, err)
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}
	}

	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

func validateStackcheckDetect(repo stackMatrixRepo, preview explainJSONPreview) (string, string) {
	missing, extra := diffStringSets(repo.ExpectedSuggestions, preview.SuggestedIntegrations)
	if len(missing) > 0 {
		return "detect_false_negative", fmt.Sprintf("missing suggested integrations: %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		return "detect_false_positive", fmt.Sprintf("unexpected suggested integrations: %s", strings.Join(extra, ", "))
	}
	return "", ""
}

func validateStackcheckContract(repo stackMatrixRepo, preview explainJSONPreview) (string, string) {
	missing, extra := diffStringSets(repo.Activate, preview.ActiveIntegrations)
	if len(missing) > 0 || len(extra) > 0 {
		var parts []string
		if len(missing) > 0 {
			parts = append(parts, "missing active integrations: "+strings.Join(missing, ", "))
		}
		if len(extra) > 0 {
			parts = append(parts, "unexpected active integrations: "+strings.Join(extra, ", "))
		}
		return "contract_mismatch", strings.Join(parts, "; ")
	}
	return "", ""
}

func diffStringSets(want, got []string) ([]string, []string) {
	wantSet := make(map[string]struct{}, len(want))
	gotSet := make(map[string]struct{}, len(got))
	for _, value := range want {
		wantSet[value] = struct{}{}
	}
	for _, value := range got {
		gotSet[value] = struct{}{}
	}

	var missing []string
	for _, value := range want {
		if _, ok := gotSet[value]; !ok {
			missing = append(missing, value)
		}
	}
	var extra []string
	for _, value := range got {
		if _, ok := wantSet[value]; !ok {
			extra = append(extra, value)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func runStackcheckExplain(selfPath, repoDir string, integrations []string) (explainJSONPreview, stackcheckCommandResult, error) {
	args := []string{"explain", "--json", "--docker=none", "-C", repoDir}
	for _, integration := range integrations {
		args = append(args, "--integration", integration)
	}
	outcome, err := runStackcheckProcess("", selfPath, args...)
	result := stackcheckCommandResultFromOutcome("explain", append([]string{selfPath}, args...), outcome)
	if err != nil {
		return explainJSONPreview{}, result, err
	}

	var preview explainJSONPreview
	if err := json.Unmarshal([]byte(outcome.stdout), &preview); err != nil {
		result.Stderr = stackcheckTrimOutput(err.Error())
		return explainJSONPreview{}, result, err
	}
	result.Stdout = ""
	return preview, result, nil
}

func runStackcheckExec(selfPath, repoDir string, integrations []string, script string, step string) (stackcheckCommandResult, error) {
	args := []string{"exec", "--docker=none", "-C", repoDir}
	for _, integration := range integrations {
		args = append(args, "--integration", integration)
	}
	args = append(args, "--", "/bin/zsh", "-lc", script)
	outcome, err := runStackcheckProcess("", selfPath, args...)
	return stackcheckCommandResultFromOutcome(step, append([]string{selfPath}, args...), outcome), err
}

func runStackcheckProcess(dir string, name string, args ...string) (stackcheckCommandOutcome, error) {
	start := time.Now()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1")

	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	outcome := stackcheckCommandOutcome{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		duration: time.Since(start),
	}
	if err == nil {
		return outcome, nil
	}

	outcome.exitCode = 1
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		outcome.exitCode = exitErr.ExitCode()
	}
	return outcome, err
}

func stackcheckCommandResultFromOutcome(step string, command []string, outcome stackcheckCommandOutcome) stackcheckCommandResult {
	return stackcheckCommandResult{
		Step:       step,
		Command:    command,
		ExitCode:   outcome.exitCode,
		DurationMS: outcome.duration.Milliseconds(),
		Stdout:     stackcheckTrimOutput(outcome.stdout),
		Stderr:     stackcheckTrimOutput(outcome.stderr),
	}
}

func classifyStackcheckProcessFailure(layer string, result stackcheckCommandResult) string {
	combined := strings.ToLower(result.Stderr + "\n" + result.Stdout)
	switch {
	case strings.Contains(combined, "not found"),
		strings.Contains(combined, "missing"),
		strings.Contains(combined, "no such file"),
		strings.Contains(combined, "timed out"):
		return "toolchain_missing"
	case layer == "workflow":
		return "workflow_failure"
	case layer == "contract":
		return "contract_mismatch"
	default:
		return "containment_failure"
	}
}

func stackcheckFailureMessage(result stackcheckCommandResult, err error) string {
	if result.Stderr != "" {
		return result.Stderr
	}
	if result.Stdout != "" {
		return result.Stdout
	}
	return err.Error()
}

func stackcheckTrimOutput(s string) string {
	s = strings.TrimSpace(s)
	const limit = 4000
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...[truncated]"
}

func summarizeStackcheckResults(results stackcheckResultSet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "hazmat: stackcheck %s\n", results.Mode)
	passed := 0
	failed := 0
	for _, result := range results.Results {
		if result.Status == stackcheckStatusPass {
			passed++
			fmt.Fprintf(&b, "  PASS %s\n", result.ID)
			continue
		}
		failed++
		fmt.Fprintf(&b, "  FAIL %s [%s] %s\n", result.ID, result.FailureClass, result.Message)
	}
	fmt.Fprintf(&b, "hazmat: stackcheck summary: %d passed, %d failed\n", passed, failed)
	return b.String()
}

func stackcheckFailureCount(results stackcheckResultSet) int {
	failures := 0
	for _, result := range results.Results {
		if result.Status == stackcheckStatusFail {
			failures++
		}
	}
	return failures
}

func ensureStackcheckRepoCheckout(workspaceRoot string, repo stackMatrixRepo) (string, error) {
	targetDir := filepath.Join(workspaceRoot, repo.ID+"-"+repo.Ref[:12])
	projectDir := filepath.Join(targetDir, repo.ProjectSubdir)
	if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
		return projectDir, nil
	}

	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return "", err
	}
	cloneDir, err := os.MkdirTemp(workspaceRoot, repo.ID+"-clone-*")
	if err != nil {
		return "", fmt.Errorf("create clone dir for %s: %w", repo.ID, err)
	}

	if _, err := runStackcheckProcess("", "git", "clone", "--filter=blob:none", "--single-branch", "--no-checkout", repo.Repo, cloneDir); err != nil {
		return "", fmt.Errorf("clone %s: %w", repo.Repo, err)
	}
	if _, err := runStackcheckProcess("", "git", "-C", cloneDir, "fetch", "--depth", "1", "origin", repo.Ref); err != nil {
		return "", fmt.Errorf("fetch %s@%s: %w", repo.ID, repo.Ref, err)
	}
	if _, err := runStackcheckProcess("", "git", "-C", cloneDir, "checkout", "--detach", repo.Ref); err != nil {
		return "", fmt.Errorf("checkout %s@%s: %w", repo.ID, repo.Ref, err)
	}
	if err := os.Rename(cloneDir, targetDir); err != nil {
		if info, statErr := os.Stat(projectDir); statErr == nil && info.IsDir() {
			return projectDir, nil
		}
		return "", fmt.Errorf("finalize checkout %s: %w", repo.ID, err)
	}

	projectDir = filepath.Join(targetDir, repo.ProjectSubdir)
	info, err := os.Stat(projectDir)
	if err != nil {
		return "", fmt.Errorf("stat project subdir %s: %w", projectDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project subdir %s is not a directory", projectDir)
	}
	return projectDir, nil
}

func defaultStackcheckWorkspaceRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "stack-matrix"
	}
	return filepath.Join(home, "workspace", "stack-matrix")
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	*target = exitErr
	return true
}
