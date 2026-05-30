//go:build hazmat_debug

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const traceFormatVersion = 1

type traceOptions struct {
	Harness    HarnessID
	OutRoot    string
	Name       string
	Syscalls   bool
	Transcript bool
}

type traceManifest struct {
	FormatVersion int       `json:"format_version"`
	Kind          string    `json:"kind"`
	Harness       HarnessID `json:"harness"`
	DisplayName   string    `json:"display_name"`
	Backend       string    `json:"backend"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at,omitempty"`
	DurationMS    int64     `json:"duration_ms,omitempty"`
	OutputDir     string    `json:"output_dir"`
	Name          string    `json:"name,omitempty"`
	ForwardedArgs []string  `json:"forwarded_args"`
	LaunchArgs    []string  `json:"launch_args"`
	Syscalls      bool      `json:"syscalls"`
	Transcript    bool      `json:"transcript"`
	ExitCode      *int      `json:"exit_code,omitempty"`
	Error         string    `json:"error,omitempty"`
}

type traceHarnessSpec struct {
	ID              HarnessID
	DisplayName     string
	CommandName     string
	Parser          harnessArgsParser
	ProcessFilters  []string
	AgentStatePaths []string
	HostStatePaths  []string
	SampleArgs      []string
}

type traceHarnessInfo struct {
	ID               HarnessID `json:"id"`
	DisplayName      string    `json:"display_name"`
	LaunchCommand    string    `json:"launch_command"`
	BootstrapCommand string    `json:"bootstrap_command,omitempty"`
	Installed        *bool     `json:"installed,omitempty"`
	ProcessFilters   []string  `json:"process_filters"`
	AgentStatePaths  []string  `json:"agent_state_paths"`
	HostStatePaths   []string  `json:"host_state_paths"`
}

type claudeTraceOptions = traceOptions

type traceBackend interface {
	name() string
	supported() bool
	unsupportedError(HarnessID) error
	observerDescription() string
	preflight(spec traceHarnessSpec, opts traceOptions) error
	writeToolProbe(dir string, spec traceHarnessSpec)
	writeHostSnapshot(dir string, spec traceHarnessSpec, phase string)
	startObservers(ctx context.Context, dir string, spec traceHarnessSpec, opts traceOptions) (traceObserverSet, error)
	runLaunch(dir string, opts traceOptions, launchArgs []string) error
	writePostLaunchLogs(dir string, spec traceHarnessSpec, start, end time.Time)
	indicatorFiles() []string
}

type traceObserverSet interface {
	waitBeforeLaunch()
	stop()
}

type noopTraceObservers struct{}

func (noopTraceObservers) waitBeforeLaunch() {}

func (noopTraceObservers) stop() {}

func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Collect diagnostic traces for contained agent launches",
		Long: `Collect diagnostic traces for contained agent launches.

Trace commands are for local debugging and audit work. They do not change the
normal Hazmat launch contract; they start host-side observers around a regular
contained session and write a trace bundle for later comparison.`,
	}
	for _, spec := range supportedTraceHarnessSpecs() {
		cmd.AddCommand(newTraceHarnessCmd(spec))
	}
	return cmd
}

func newTraceHarnessCmd(spec traceHarnessSpec) *cobra.Command {
	backend := currentTraceBackend()
	opts := traceOptions{
		Harness:    spec.ID,
		Syscalls:   true,
		Transcript: true,
	}

	cmd := &cobra.Command{
		Use:   string(spec.ID) + " [trace-flags] -- [hazmat-" + spec.CommandName + "-flags] [" + spec.CommandName + "-args...]",
		Short: "Trace a regular hazmat " + spec.CommandName + " launch",
		Long: fmt.Sprintf(`Trace a regular hazmat %s launch.

The command starts %s before launching %s through the
same public Hazmat entrypoint used by `+"`hazmat %s`"+`. Results are written
to a timestamped directory under ~/.hazmat/traces unless --out is provided.

Put Hazmat/%s launch flags after -- so they are forwarded untouched.

Examples:
  hazmat trace %s -- %s
  hazmat trace %s --name baseline -- --no-backup %s
  hazmat trace %s --name network-none -- --network none %s`,
			spec.CommandName,
			backend.observerDescription(),
			spec.DisplayName,
			spec.CommandName,
			spec.DisplayName,
			spec.CommandName, strings.Join(shellQuote(spec.SampleArgs), " "),
			spec.CommandName, strings.Join(shellQuote(spec.SampleArgs), " "),
			spec.CommandName, strings.Join(shellQuote(spec.SampleArgs), " ")),
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return runHarnessTrace(opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.OutRoot, "out", "",
		"Directory in which to create the trace bundle (default: ~/.hazmat/traces)")
	cmd.Flags().StringVar(&opts.Name, "name", "",
		"Short experiment label to include in the trace directory name")
	return cmd
}

func runHarnessTrace(opts traceOptions, forwarded []string) error {
	backend := currentTraceBackend()
	if !backend.supported() {
		return backend.unsupportedError(opts.Harness)
	}

	spec, ok := traceHarnessSpecByID(opts.Harness)
	if !ok {
		return fmt.Errorf("unknown trace harness %q", opts.Harness)
	}
	if err := preflightTraceRuntime(opts); err != nil {
		return err
	}
	if err := backend.preflight(spec, opts); err != nil {
		return err
	}

	start := time.Now()
	traceDir, label, err := prepareTraceDir(opts, start)
	if err != nil {
		return err
	}

	launchArgs := traceLaunchArgs(spec, forwarded)
	manifest := traceManifest{
		FormatVersion: traceFormatVersion,
		Kind:          "hazmat.harness_trace",
		Harness:       spec.ID,
		DisplayName:   spec.DisplayName,
		Backend:       backend.name(),
		StartedAt:     start,
		OutputDir:     traceDir,
		Name:          label,
		ForwardedArgs: append([]string(nil), forwarded...),
		LaunchArgs:    append([]string(nil), launchArgs...),
		Syscalls:      opts.Syscalls,
		Transcript:    opts.Transcript,
	}
	writeTraceManifest(traceDir, manifest)

	fmt.Fprintf(os.Stderr, "hazmat trace: writing %s trace bundle to %s\n", spec.DisplayName, traceDir)
	writeTraceText(traceDir, "command.txt", strings.Join(shellQuote(append([]string{"hazmat"}, launchArgs...)), " ")+"\n")
	writeTraceExperimentGuide(traceDir, spec, backend)
	writeTraceHarnessInfo(traceDir, spec)
	backend.writeHostSnapshot(traceDir, spec, "before")
	writeTraceExplain(traceDir, spec, forwarded)
	backend.writeToolProbe(traceDir, spec)

	ctx, cancel := context.WithCancel(context.Background())
	observers, err := backend.startObservers(ctx, traceDir, spec, opts)
	if err != nil {
		cancel()
		return err
	}
	if observers == nil {
		observers = noopTraceObservers{}
	}
	observers.waitBeforeLaunch()

	launchErr := backend.runLaunch(traceDir, opts, launchArgs)
	end := time.Now()

	cancel()
	observers.stop()
	backend.writeHostSnapshot(traceDir, spec, "after")
	backend.writePostLaunchLogs(traceDir, spec, start, end)
	writeTraceIndicators(traceDir, backend.indicatorFiles())

	manifest.EndedAt = end
	manifest.DurationMS = end.Sub(start).Milliseconds()
	if launchErr != nil {
		manifest.Error = launchErr.Error()
		if code, ok := commandExitCode(launchErr); ok {
			manifest.ExitCode = &code
		}
	} else {
		code := 0
		manifest.ExitCode = &code
	}
	writeTraceManifest(traceDir, manifest)

	fmt.Fprintf(os.Stderr, "hazmat trace: bundle complete: %s\n", traceDir)
	return launchErr
}

func preflightTraceRuntime(opts traceOptions) error {
	if opts.Transcript {
		if hostScriptPath == "" {
			return fmt.Errorf("trace requires script(1) for terminal transcript capture")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("trace requires terminal stdin so script(1) can capture a full PTY transcript")
		}
	}
	return nil
}

func claudeTraceLaunchArgs(forwarded []string) []string {
	spec, _ := traceHarnessSpecByID(HarnessClaude)
	return traceLaunchArgs(spec, forwarded)
}

func traceLaunchArgs(spec traceHarnessSpec, forwarded []string) []string {
	args := []string{spec.CommandName, "--yes"}
	return append(args, forwarded...)
}

func prepareClaudeTraceDir(opts claudeTraceOptions, now time.Time) (string, string, error) {
	opts.Harness = HarnessClaude
	return prepareTraceDir(opts, now)
}

func prepareTraceDir(opts traceOptions, now time.Time) (string, string, error) {
	spec, ok := traceHarnessSpecByID(opts.Harness)
	if !ok {
		return "", "", fmt.Errorf("unknown trace harness %q", opts.Harness)
	}
	root := opts.OutRoot
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("determine home directory for trace output: %w", err)
		}
		root = filepath.Join(home, ".hazmat", "traces")
	}
	root = expandTilde(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", "", fmt.Errorf("create trace root %s: %w", root, err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", "", fmt.Errorf("set trace root mode %s: %w", root, err)
	}

	label := sanitizeTraceLabel(opts.Name)
	name := now.UTC().Format("20060102T150405Z") + "-" + string(spec.ID)
	if label != "" {
		name += "-" + label
	}
	dir := filepath.Join(root, name)
	for i := 0; ; i++ {
		candidate := dir
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", dir, i)
		}
		if err := os.Mkdir(candidate, 0o700); err == nil {
			return candidate, label, nil
		} else if !os.IsExist(err) {
			return "", "", fmt.Errorf("create trace directory %s: %w", candidate, err)
		}
	}
}

func sanitizeTraceLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		var out rune
		switch {
		case r == '.' || r == '_' || r == '-':
			out = r
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out = unicode.ToLower(r)
		case unicode.IsSpace(r):
			out = '-'
		default:
			out = '-'
		}
		if out == '-' {
			if lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		b.WriteRune(out)
		if b.Len() >= 80 {
			break
		}
	}
	return strings.Trim(b.String(), "-._")
}

func writeTraceManifest(dir string, manifest traceManifest) {
	writeTraceJSON(dir, "manifest.json", manifest)
}

func supportedTraceHarnessSpecs() []traceHarnessSpec {
	return []traceHarnessSpec{
		{
			ID:          HarnessClaude,
			DisplayName: "Claude Code",
			CommandName: "claude",
			Parser:      parseClaudeArgs,
			ProcessFilters: []string{
				"claude",
				"2.1.",
				"com.anthropic",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".claude"),
				filepath.Join(agentHome, ".claude.json"),
				filepath.Join(agentHome, ".local", "share", "claude"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/claude",
			},
			SampleArgs: []string{"-p", "say ok"},
		},
		{
			ID:          HarnessCodex,
			DisplayName: "Codex",
			CommandName: "codex",
			Parser:      parseHarnessArgs,
			ProcessFilters: []string{
				"codex",
				"com.openai.codex",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".codex"),
				filepath.Join(agentHome, ".agents"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/codex",
			},
			SampleArgs: []string{"exec", "say ok"},
		},
		{
			ID:          HarnessOpenCode,
			DisplayName: "OpenCode",
			CommandName: "opencode",
			Parser:      parseHarnessArgs,
			ProcessFilters: []string{
				"opencode",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".opencode"),
				filepath.Join(agentHome, ".config", "opencode"),
				filepath.Join(agentHome, ".local", "share", "opencode"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/opencode",
			},
			SampleArgs: []string{"run", "say ok"},
		},
		{
			ID:          HarnessGemini,
			DisplayName: "Gemini",
			CommandName: "gemini",
			Parser:      parseHarnessArgs,
			ProcessFilters: []string{
				"gemini",
			},
			AgentStatePaths: []string{
				filepath.Join(agentHome, ".gemini"),
			},
			HostStatePaths: []string{
				"~/.hazmat/secrets/gemini",
			},
			SampleArgs: []string{"-p", "say ok"},
		},
	}
}

func traceHarnessSpecByID(id HarnessID) (traceHarnessSpec, bool) {
	for _, spec := range supportedTraceHarnessSpecs() {
		if spec.ID == id {
			return spec, true
		}
	}
	return traceHarnessSpec{}, false
}

func writeTraceHarnessInfo(dir string, spec traceHarnessSpec) {
	info := traceHarnessInfo{
		ID:              spec.ID,
		DisplayName:     spec.DisplayName,
		LaunchCommand:   "hazmat " + spec.CommandName,
		ProcessFilters:  append([]string(nil), spec.ProcessFilters...),
		AgentStatePaths: append([]string(nil), spec.AgentStatePaths...),
		HostStatePaths:  append([]string(nil), spec.HostStatePaths...),
	}
	for _, managed := range managedHarnessRegistry {
		if managed.Spec.ID != spec.ID {
			continue
		}
		info.LaunchCommand = managed.LaunchCommand
		info.BootstrapCommand = managed.BootstrapCommand
		if managed.Installed != nil {
			installed := managed.Installed()
			info.Installed = &installed
		}
		break
	}
	writeTraceJSON(dir, "harness.json", info)
}

func writeTraceExplain(dir string, spec traceHarnessSpec, forwarded []string) {
	opts, _, err := spec.Parser(forwarded)
	if err != nil {
		writeTraceText(dir, "explain-error.txt", err.Error()+"\n")
		return
	}
	opts.planOnly = true
	cfg, mode, err := resolveExplainSession(spec.CommandName, opts)
	if err != nil {
		writeTraceText(dir, "explain-error.txt", err.Error()+"\n")
		return
	}
	writeTraceJSON(dir, "explain.json", buildExplainJSON(spec.CommandName, cfg, mode, opts.noBackup))
}

func sanitizeTraceFilename(path string) string {
	path = strings.TrimPrefix(path, "~/")
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, string(os.PathSeparator), "-")
	return sanitizeTraceLabel(path)
}

func runTraceLaunch(dir string, opts traceOptions, launchArgs []string) error {
	cmd, err := newTraceLaunchCommand(dir, opts, launchArgs)
	if err != nil {
		return err
	}
	return runSessionCommand(cmd)
}

func newTraceLaunchCommand(dir string, opts traceOptions, launchArgs []string) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable for traced launch: %w", err)
	}

	var cmd *exec.Cmd
	if opts.Transcript {
		transcript := filepath.Join(dir, "terminal.typescript")
		args := append([]string{"-q", transcript, self}, launchArgs...)
		cmd = exec.Command(hostScriptPath, args...)
	} else {
		cmd = exec.Command(self, launchArgs...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = "/"
	return cmd, nil
}

func processSampleLineRelevant(line string, spec traceHarnessSpec) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "hazmat") ||
		strings.Contains(line, strings.TrimRight(agentHome, "/")+"/") ||
		strings.Contains(line, " "+agentUser+" ") {
		return true
	}
	for _, filter := range spec.ProcessFilters {
		if filter != "" && strings.Contains(lower, strings.ToLower(filter)) {
			return true
		}
	}
	return false
}

func writeTraceIndicators(dir string, files []string) {
	patterns := []string{
		"sandbox",
		"automation",
		"permission",
		"denied",
		" eacces",
		" eperm",
		"auth",
		"oauth",
		"logout",
		"keychain",
		"sysctl",
		"csops",
		"ptrace",
		"proc_pid",
		"getxattr",
		"quarantine",
		"seatbelt",
	}
	var b strings.Builder
	for _, file := range files {
		for _, path := range traceIndicatorPaths(dir, file) {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			display := file
			if rel, err := filepath.Rel(dir, path); err == nil {
				display = rel
			}
			fmt.Fprintf(&b, "## %s\n", display)
			scanner := bufio.NewScanner(f)
			count := 0
			for scanner.Scan() {
				line := scanner.Text()
				lower := strings.ToLower(line)
				for _, pattern := range patterns {
					if strings.Contains(lower, pattern) {
						fmt.Fprintln(&b, line)
						count++
						break
					}
				}
				if count >= 300 {
					fmt.Fprintln(&b, "... truncated at 300 indicator lines ...")
					break
				}
			}
			if count == 0 {
				fmt.Fprintln(&b, "(no indicator lines matched)")
			}
			fmt.Fprintln(&b)
			_ = f.Close()
		}
	}
	writeTraceText(dir, "indicators.md", b.String())
}

func traceIndicatorPaths(dir, file string) []string {
	if !strings.ContainsAny(file, "*?[") {
		return []string{filepath.Join(dir, file)}
	}
	matches, err := filepath.Glob(filepath.Join(dir, file))
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)
	return matches
}

func writeTraceExperimentGuide(dir string, spec traceHarnessSpec, backend traceBackend) {
	sample := strings.Join(shellQuote(spec.SampleArgs), " ")
	artifacts := append([]string{
		"manifest.json",
		"harness.json",
		"explain.json",
		"command.txt",
	}, backend.indicatorFiles()...)
	artifactList := strings.Join(artifacts, ", ")
	content := fmt.Sprintf(`# %[1]s Trace Experiment Notes

Run each experiment with a short non-interactive %[1]s prompt first, then only
move to an interactive reproduction once the baseline is understood.

Suggested sequence:

1. Baseline: hazmat trace %[2]s --name baseline -- --no-backup %[3]s
2. No network: hazmat trace %[2]s --name network-none -- --no-backup --network none %[3]s
3. Docker Sandbox, if relevant: hazmat trace %[2]s --name docker -- --no-backup --docker=sandbox %[3]s
4. If this harness has Hazmat-managed permission bypass behavior, temporarily
   disable it with:
   hazmat config set session.skip_permissions false
   Then rerun the baseline and restore it with:
   hazmat config set session.skip_permissions true

Compare %[4]s, and indicators.md across runs. The strongest evidence is a
probe or denial that appears only in failing runs and disappears in the nearest
passing control.
`, spec.DisplayName, spec.CommandName, sample, artifactList)
	writeTraceText(dir, "experiments.md", content)
}

func runTraceCommandToFile(path string, timeout time.Duration, name string, args ...string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "# %s\n# %s\n\n", time.Now().Format(time.RFC3339Nano), strings.Join(shellQuote(append([]string{name}, args...)), " "))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = file
	cmd.Stderr = file
	err = cmd.Run()
	if ctx.Err() != nil {
		fmt.Fprintf(file, "\n# timed out after %s\n", timeout)
		return
	}
	if err != nil {
		fmt.Fprintf(file, "\n# exit error: %v\n", err)
	}
}

func requireTraceExecutable(label, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %s: %w", label, path, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s %s is not executable", label, path)
	}
	return nil
}

func requireTraceReadablePath(label, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s %s: %w", label, path, err)
	}
	_ = file.Close()
	return nil
}

func runTracePreflightCommand(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("%s timed out after %s", strings.Join(shellQuote(append([]string{name}, args...)), " "), timeout)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", strings.Join(shellQuote(append([]string{name}, args...)), " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeTraceJSON(dir, name string, value any) {
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

func writeTraceText(dir, name, content string) {
	_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}

func appendTraceText(dir, name, content string) {
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(content)
}

func commandExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}
