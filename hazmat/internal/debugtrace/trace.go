//go:build hazmat_debug

package debugtrace

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

type Env struct {
	AgentHome         string
	AgentUser         string
	DefaultAgentPath  string
	HostLsPath        string
	HostLogPath       string
	HostScriptPath    string
	HostSudoPath      string
	HostUnamePath     string
	ExpandTilde       func(string) string
	RunSessionCommand func(*exec.Cmd) error
}

type HarnessID string

type Options struct {
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

type HarnessSpec struct {
	ID               HarnessID
	DisplayName      string
	CommandName      string
	LaunchCommand    string
	BootstrapCommand string
	Installed        func() bool
	Explain          func(forwarded []string) (any, error)
	ProcessFilters   []string
	AgentStatePaths  []string
	HostStatePaths   []string
	SampleArgs       []string
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

type traceBackend interface {
	name() string
	supported() bool
	unsupportedError(HarnessID) error
	observerDescription() string
	preflight(env Env, spec HarnessSpec, opts Options) error
	writeToolProbe(env Env, dir string, spec HarnessSpec) error
	writeHostSnapshot(env Env, dir string, spec HarnessSpec, phase string) error
	startObservers(ctx context.Context, env Env, dir string, spec HarnessSpec, opts Options) (traceObserverSet, error)
	runLaunch(env Env, dir string, opts Options, launchArgs []string) error
	writePostLaunchLogs(env Env, dir string, spec HarnessSpec, start, end time.Time) error
	indicatorFiles() []string
}

type traceObserverSet interface {
	waitBeforeLaunch()
	stop()
}

type noopTraceObservers struct{}

func (noopTraceObservers) waitBeforeLaunch() {}

func (noopTraceObservers) stop() {}

func NewCommand(env Env, specs []HarnessSpec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Collect diagnostic traces for contained agent launches",
		Long: `Collect diagnostic traces for contained agent launches.

	Trace commands are for local debugging and audit work. They do not change the
normal Hazmat launch contract; they start host-side observers around a regular
contained session and write a trace bundle for later comparison.`,
	}
	for _, spec := range specs {
		cmd.AddCommand(newTraceHarnessCmd(env, specs, spec))
	}
	return cmd
}

func newTraceHarnessCmd(env Env, specs []HarnessSpec, spec HarnessSpec) *cobra.Command {
	backend := currentTraceBackend()
	opts := Options{
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
			spec.CommandName, strings.Join(ShellQuote(spec.SampleArgs), " "),
			spec.CommandName, strings.Join(ShellQuote(spec.SampleArgs), " "),
			spec.CommandName, strings.Join(ShellQuote(spec.SampleArgs), " ")),
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return RunHarnessTrace(env, specs, opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.OutRoot, "out", "",
		"Directory in which to create the trace bundle (default: ~/.hazmat/traces)")
	cmd.Flags().StringVar(&opts.Name, "name", "",
		"Short experiment label to include in the trace directory name")
	return cmd
}

func RunHarnessTrace(env Env, specs []HarnessSpec, opts Options, forwarded []string) error {
	backend := currentTraceBackend()
	if !backend.supported() {
		return backend.unsupportedError(opts.Harness)
	}

	spec, ok := HarnessSpecByID(specs, opts.Harness)
	if !ok {
		return fmt.Errorf("unknown trace harness %q", opts.Harness)
	}
	if err := preflightTraceRuntime(env, opts); err != nil {
		return err
	}
	if err := backend.preflight(env, spec, opts); err != nil {
		return err
	}

	start := time.Now()
	traceDir, label, err := PrepareTraceDir(env, specs, opts, start)
	if err != nil {
		return err
	}

	launchArgs := TraceLaunchArgs(spec, forwarded)
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
	if err := writeTraceManifest(traceDir, manifest); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "hazmat trace: writing %s trace bundle to %s\n", spec.DisplayName, traceDir)
	if err := writeTraceText(traceDir, "command.txt", strings.Join(ShellQuote(append([]string{"hazmat"}, launchArgs...)), " ")+"\n"); err != nil {
		return err
	}
	if err := writeTraceExperimentGuide(traceDir, spec, backend); err != nil {
		return err
	}
	if err := writeTraceHarnessInfo(traceDir, spec); err != nil {
		return err
	}
	if err := backend.writeHostSnapshot(env, traceDir, spec, "before"); err != nil {
		return err
	}
	if err := writeTraceExplain(traceDir, spec, forwarded); err != nil {
		return err
	}
	if err := backend.writeToolProbe(env, traceDir, spec); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	observers, err := backend.startObservers(ctx, env, traceDir, spec, opts)
	if err != nil {
		cancel()
		return err
	}
	if observers == nil {
		observers = noopTraceObservers{}
	}
	observers.waitBeforeLaunch()

	launchErr := backend.runLaunch(env, traceDir, opts, launchArgs)
	end := time.Now()
	traceErr := launchErr

	cancel()
	observers.stop()
	if err := backend.writeHostSnapshot(env, traceDir, spec, "after"); err != nil {
		traceErr = errors.Join(traceErr, err)
	}
	if err := backend.writePostLaunchLogs(env, traceDir, spec, start, end); err != nil {
		traceErr = errors.Join(traceErr, err)
	}
	if err := writeTraceIndicators(traceDir, backend.indicatorFiles()); err != nil {
		traceErr = errors.Join(traceErr, err)
	}

	manifest.EndedAt = end
	manifest.DurationMS = end.Sub(start).Milliseconds()
	if traceErr != nil {
		manifest.Error = traceErr.Error()
		if code, ok := commandExitCode(launchErr); ok {
			manifest.ExitCode = &code
		}
	} else {
		code := 0
		manifest.ExitCode = &code
	}
	if err := writeTraceManifest(traceDir, manifest); err != nil {
		return errors.Join(traceErr, err)
	}

	fmt.Fprintf(os.Stderr, "hazmat trace: bundle complete: %s\n", traceDir)
	return traceErr
}

func preflightTraceRuntime(env Env, opts Options) error {
	if !opts.Syscalls {
		return fmt.Errorf("trace requires syscall observer capture")
	}
	if !opts.Transcript {
		return fmt.Errorf("trace requires PTY transcript capture")
	}
	if opts.Transcript {
		if env.HostScriptPath == "" {
			return fmt.Errorf("trace requires script(1) for terminal transcript capture")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("trace requires terminal stdin so script(1) can capture a full PTY transcript")
		}
	}
	return nil
}

func TraceLaunchArgs(spec HarnessSpec, forwarded []string) []string {
	args := []string{spec.CommandName, "--yes"}
	return append(args, forwarded...)
}

func PrepareTraceDir(env Env, specs []HarnessSpec, opts Options, now time.Time) (string, string, error) {
	spec, ok := HarnessSpecByID(specs, opts.Harness)
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
	if env.ExpandTilde != nil {
		root = env.ExpandTilde(root)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", "", fmt.Errorf("create trace root %s: %w", root, err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", "", fmt.Errorf("set trace root mode %s: %w", root, err)
	}

	label := SanitizeLabel(opts.Name)
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

func SanitizeLabel(raw string) string {
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

func writeTraceManifest(dir string, manifest traceManifest) error {
	return writeTraceJSON(dir, "manifest.json", manifest)
}

func HarnessSpecByID(specs []HarnessSpec, id HarnessID) (HarnessSpec, bool) {
	for _, spec := range specs {
		if spec.ID == id {
			return spec, true
		}
	}
	return HarnessSpec{}, false
}

func writeTraceHarnessInfo(dir string, spec HarnessSpec) error {
	info := traceHarnessInfo{
		ID:               spec.ID,
		DisplayName:      spec.DisplayName,
		LaunchCommand:    spec.LaunchCommand,
		BootstrapCommand: spec.BootstrapCommand,
		ProcessFilters:   append([]string(nil), spec.ProcessFilters...),
		AgentStatePaths:  append([]string(nil), spec.AgentStatePaths...),
		HostStatePaths:   append([]string(nil), spec.HostStatePaths...),
	}
	if info.LaunchCommand == "" {
		info.LaunchCommand = "hazmat " + spec.CommandName
	}
	if spec.Installed != nil {
		installed := spec.Installed()
		info.Installed = &installed
	}
	return writeTraceJSON(dir, "harness.json", info)
}

func writeTraceExplain(dir string, spec HarnessSpec, forwarded []string) error {
	if spec.Explain == nil {
		return writeTraceText(dir, "explain-error.txt", "trace explain callback is not configured\n")
	}
	explain, err := spec.Explain(forwarded)
	if err != nil {
		return writeTraceText(dir, "explain-error.txt", err.Error()+"\n")
	}
	return writeTraceJSON(dir, "explain.json", explain)
}

func sanitizeTraceFilename(path string) string {
	path = strings.TrimPrefix(path, "~/")
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, string(os.PathSeparator), "-")
	return SanitizeLabel(path)
}

func runTraceLaunch(env Env, dir string, opts Options, launchArgs []string) error {
	cmd, err := newTraceLaunchCommand(env, dir, opts, launchArgs)
	if err != nil {
		return err
	}
	if env.RunSessionCommand == nil {
		return fmt.Errorf("trace run session callback is not configured")
	}
	return env.RunSessionCommand(cmd)
}

func newTraceLaunchCommand(env Env, dir string, opts Options, launchArgs []string) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable for traced launch: %w", err)
	}

	var cmd *exec.Cmd
	if opts.Transcript {
		transcript := filepath.Join(dir, "terminal.typescript")
		cmd = exec.Command(env.HostScriptPath, traceScriptCommandArgs(transcript, self, launchArgs)...)
	} else {
		cmd = exec.Command(self, launchArgs...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = "/"
	return cmd, nil
}

func processSampleLineRelevant(env Env, line string, spec HarnessSpec) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "hazmat") ||
		strings.Contains(line, strings.TrimRight(env.AgentHome, "/")+"/") ||
		strings.Contains(line, " "+env.AgentUser+" ") {
		return true
	}
	for _, filter := range spec.ProcessFilters {
		if filter != "" && strings.Contains(lower, strings.ToLower(filter)) {
			return true
		}
	}
	return false
}

func writeTraceIndicators(dir string, files []string) error {
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
	return writeTraceText(dir, "indicators.md", b.String())
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

func writeTraceExperimentGuide(dir string, spec HarnessSpec, backend traceBackend) error {
	sample := strings.Join(ShellQuote(spec.SampleArgs), " ")
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
	return writeTraceText(dir, "experiments.md", content)
}

func runTraceCommandToFile(path string, timeout time.Duration, name string, args ...string) error {
	return runTraceCommandToFileMode(path, timeout, false, name, args...)
}

func runRequiredTraceCommandToFile(path string, timeout time.Duration, name string, args ...string) error {
	return runTraceCommandToFileMode(path, timeout, true, name, args...)
}

func runTraceCommandToFileMode(path string, timeout time.Duration, requireSuccess bool, name string, args ...string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create trace command output %s: %w", path, err)
	}
	var result error
	commandText := strings.Join(ShellQuote(append([]string{name}, args...)), " ")
	if _, err := fmt.Fprintf(file, "# %s\n# %s\n\n", time.Now().Format(time.RFC3339Nano), commandText); err != nil {
		result = fmt.Errorf("write trace command header %s: %w", path, err)
		if closeErr := file.Close(); closeErr != nil {
			result = errors.Join(result, fmt.Errorf("close trace command output %s: %w", path, closeErr))
		}
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = file
	cmd.Stderr = file
	err = cmd.Run()
	if ctx.Err() != nil {
		timeoutErr := fmt.Errorf("trace command %s timed out after %s", commandText, timeout)
		if _, writeErr := fmt.Fprintf(file, "\n# timed out after %s\n", timeout); writeErr != nil {
			result = errors.Join(timeoutErr, fmt.Errorf("write trace command timeout %s: %w", path, writeErr))
		} else {
			result = timeoutErr
		}
	} else if err != nil {
		if _, writeErr := fmt.Fprintf(file, "\n# exit error: %v\n", err); writeErr != nil {
			result = fmt.Errorf("write trace command error %s: %w", path, writeErr)
		} else if requireSuccess {
			result = fmt.Errorf("trace command %s failed: %w", commandText, err)
		}
	}
	if closeErr := file.Close(); closeErr != nil {
		result = errors.Join(result, fmt.Errorf("close trace command output %s: %w", path, closeErr))
	}
	return result
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
		return fmt.Errorf("%s timed out after %s", strings.Join(ShellQuote(append([]string{name}, args...)), " "), timeout)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", strings.Join(ShellQuote(append([]string{name}, args...)), " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeTraceJSON(dir, name string, value any) error {
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create trace JSON %s: %w", path, err)
	}
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	var result error
	if err := enc.Encode(value); err != nil {
		result = fmt.Errorf("write trace JSON %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		result = errors.Join(result, fmt.Errorf("close trace JSON %s: %w", path, err))
	}
	return result
}

func writeTraceText(dir, name, content string) error {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write trace text %s: %w", path, err)
	}
	return nil
}

func commandExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

func ShellQuote(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'`$\\{}()|&;<>") {
			out[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
		} else {
			out[i] = arg
		}
	}
	return out
}
