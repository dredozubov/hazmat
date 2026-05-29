package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const claudeTraceFormatVersion = 1

type claudeTraceOptions struct {
	OutRoot    string
	Name       string
	Syscalls   bool
	Transcript bool
}

type claudeTraceManifest struct {
	FormatVersion int       `json:"format_version"`
	Kind          string    `json:"kind"`
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

func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Collect diagnostic traces for contained agent launches",
		Long: `Collect diagnostic traces for contained agent launches.

Trace commands are for local debugging and audit work. They do not change the
normal Hazmat launch contract; they start host-side observers around a regular
contained session and write a trace bundle for later comparison.`,
	}
	cmd.AddCommand(newTraceClaudeCmd())
	return cmd
}

func newTraceClaudeCmd() *cobra.Command {
	opts := claudeTraceOptions{
		Syscalls:   true,
		Transcript: true,
	}
	var noSyscalls bool
	var noTranscript bool

	cmd := &cobra.Command{
		Use:   "claude [trace-flags] -- [hazmat-claude-flags] [claude-args...]",
		Short: "Trace a regular hazmat claude launch",
		Long: `Trace a regular hazmat claude launch.

The command starts macOS observers before launching Claude Code through the
same public Hazmat entrypoint used by ` + "`hazmat claude`" + `. Results are written
to a timestamped directory under ~/.hazmat/traces unless --out is provided.

Put Hazmat/Claude launch flags after -- so they are forwarded untouched.

Examples:
  hazmat trace claude -- -p "say ok"
  hazmat trace claude --name baseline -- --no-backup -p "say ok"
  hazmat trace claude --no-syscalls -- --network none -p "offline check"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if noSyscalls {
				opts.Syscalls = false
			}
			if noTranscript {
				opts.Transcript = false
			}
			return runClaudeTrace(opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.OutRoot, "out", "",
		"Directory in which to create the trace bundle (default: ~/.hazmat/traces)")
	cmd.Flags().StringVar(&opts.Name, "name", "",
		"Short experiment label to include in the trace directory name")
	cmd.Flags().BoolVar(&opts.Syscalls, "syscalls", true,
		"Attempt host-side syscall/filesystem probes with sudo -n dtruss/fs_usage/opensnoop")
	cmd.Flags().BoolVar(&noSyscalls, "no-syscalls", false,
		"Disable host-side syscall/filesystem probes")
	cmd.Flags().BoolVar(&opts.Transcript, "transcript", true,
		"Capture terminal I/O with script(1) while preserving a PTY")
	cmd.Flags().BoolVar(&noTranscript, "no-transcript", false,
		"Disable terminal transcript capture")
	return cmd
}

func runClaudeTrace(opts claudeTraceOptions, forwarded []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("hazmat trace claude is currently implemented for macOS/Darwin only")
	}

	start := time.Now()
	traceDir, label, err := prepareClaudeTraceDir(opts, start)
	if err != nil {
		return err
	}

	launchArgs := claudeTraceLaunchArgs(forwarded)
	manifest := claudeTraceManifest{
		FormatVersion: claudeTraceFormatVersion,
		Kind:          "hazmat.claude_trace",
		StartedAt:     start,
		OutputDir:     traceDir,
		Name:          label,
		ForwardedArgs: append([]string(nil), forwarded...),
		LaunchArgs:    append([]string(nil), launchArgs...),
		Syscalls:      opts.Syscalls,
		Transcript:    opts.Transcript,
	}
	writeClaudeTraceManifest(traceDir, manifest)

	fmt.Fprintf(os.Stderr, "hazmat trace: writing Claude trace bundle to %s\n", traceDir)
	writeTraceText(traceDir, "command.txt", strings.Join(shellQuote(append([]string{"hazmat"}, launchArgs...)), " ")+"\n")
	writeTraceExperimentGuide(traceDir)
	writeTraceHostSnapshot(traceDir, "before")
	writeTraceExplain(traceDir, forwarded)
	writeTraceToolProbe(traceDir)

	ctx, cancel := context.WithCancel(context.Background())
	var samplerDone <-chan struct{}
	if opts.Syscalls {
		samplerDone = startClaudeTraceProcessSampler(ctx, filepath.Join(traceDir, "process-samples.log"))
	}
	probes := startClaudeTraceProbes(opts.Syscalls, traceDir)
	if opts.Syscalls {
		time.Sleep(750 * time.Millisecond)
	}

	launchErr := runClaudeTraceLaunch(traceDir, opts, launchArgs)
	end := time.Now()

	cancel()
	if samplerDone != nil {
		<-samplerDone
	}
	stopClaudeTraceProbes(probes)
	writeTraceHostSnapshot(traceDir, "after")
	writeTraceUnifiedLogs(traceDir, start, end)
	writeTraceIndicators(traceDir)

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
	writeClaudeTraceManifest(traceDir, manifest)

	fmt.Fprintf(os.Stderr, "hazmat trace: bundle complete: %s\n", traceDir)
	return launchErr
}

func claudeTraceLaunchArgs(forwarded []string) []string {
	args := []string{"claude", "--yes"}
	return append(args, forwarded...)
}

func prepareClaudeTraceDir(opts claudeTraceOptions, now time.Time) (string, string, error) {
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
	name := now.UTC().Format("20060102T150405Z") + "-claude"
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

func writeClaudeTraceManifest(dir string, manifest claudeTraceManifest) {
	writeTraceJSON(dir, "manifest.json", manifest)
}

func writeTraceExplain(dir string, forwarded []string) {
	opts, _, err := parseClaudeArgs(forwarded)
	if err != nil {
		writeTraceText(dir, "explain-error.txt", err.Error()+"\n")
		return
	}
	opts.planOnly = true
	cfg, mode, err := resolveExplainSession("claude", opts)
	if err != nil {
		writeTraceText(dir, "explain-error.txt", err.Error()+"\n")
		return
	}
	writeTraceJSON(dir, "explain.json", buildExplainJSON("claude", cfg, mode, opts.noBackup))
}

func writeTraceToolProbe(dir string) {
	type probe struct {
		Name string   `json:"name"`
		Args []string `json:"args"`
	}
	probes := []probe{
		{Name: hostUnamePath, Args: []string{"-a"}},
		{Name: "/usr/bin/sw_vers"},
		{Name: "/usr/bin/csrutil", Args: []string{"status"}},
		{Name: "/usr/bin/which", Args: []string{"dtruss", "fs_usage", "opensnoop", "execsnoop", "sample", "spindump", "script", "log"}},
		{Name: hostSudoPath, Args: []string{"-n", "-v"}},
		{Name: hostSudoPath, Args: []string{"-n", "/usr/bin/dtruss", "/usr/bin/true"}},
	}
	for i, p := range probes {
		name := fmt.Sprintf("tool-probe-%02d-%s.txt", i+1, filepath.Base(p.Name))
		runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second, p.Name, p.Args...)
	}
}

func writeTraceHostSnapshot(dir, phase string) {
	runTraceCommandToFile(filepath.Join(dir, phase+"-ps.txt"), 10*time.Second,
		"/bin/ps", "-axo", "pid,ppid,pgid,user,stat,etime,command")
	runTraceCommandToFile(filepath.Join(dir, phase+"-agent-claude-ls.txt"), 10*time.Second,
		hostSudoPath, "-n", "-u", agentUser, hostLsPath, "-laeO@", filepath.Join(agentHome, ".claude"))
	runTraceCommandToFile(filepath.Join(dir, phase+"-agent-config-ls.txt"), 10*time.Second,
		hostSudoPath, "-n", "-u", agentUser, hostLsPath, "-laeO@", filepath.Join(agentHome, ".config"))
	if home, err := os.UserHomeDir(); err == nil {
		runTraceCommandToFile(filepath.Join(dir, phase+"-host-secret-store-ls.txt"), 10*time.Second,
			hostLsPath, "-laeO@", filepath.Join(home, ".hazmat", "secrets", "claude"))
	}
}

func runClaudeTraceLaunch(dir string, opts claudeTraceOptions, launchArgs []string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable for traced launch: %w", err)
	}

	var cmd *exec.Cmd
	if opts.Transcript && hostScriptPath != "" && term.IsTerminal(int(os.Stdin.Fd())) {
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
	return runSessionCommand(cmd)
}

type claudeTraceProbe struct {
	Name string
	Cmd  *exec.Cmd
	File *os.File
}

func startClaudeTraceProbes(enabled bool, dir string) []claudeTraceProbe {
	if !enabled {
		return nil
	}
	specs := []struct {
		name string
		file string
		argv []string
	}{
		{
			name: "dtruss",
			file: "dtruss.log",
			argv: []string{"/usr/bin/dtruss", "-f", "-a", "-d", "-e", "-l", "-W", "claude"},
		},
		{
			name: "fs_usage",
			file: "fs_usage.log",
			argv: []string{"/usr/bin/fs_usage", "-w", "-f", "filesys", "claude"},
		},
		{
			name: "opensnoop",
			file: "opensnoop.log",
			argv: []string{"/usr/bin/opensnoop", "-n", "claude"},
		},
	}

	var probes []claudeTraceProbe
	for _, spec := range specs {
		probe, err := startClaudeTraceProbe(spec.name, filepath.Join(dir, spec.file), spec.argv)
		if err != nil {
			appendTraceText(dir, "trace-errors.log", fmt.Sprintf("%s: %v\n", spec.name, err))
			continue
		}
		probes = append(probes, probe)
	}
	return probes
}

func startClaudeTraceProbe(name, path string, argv []string) (claudeTraceProbe, error) {
	if len(argv) == 0 {
		return claudeTraceProbe{}, fmt.Errorf("missing argv")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return claudeTraceProbe{}, err
	}
	full := sudoNoPromptTraceArgv(argv)
	fmt.Fprintf(file, "# %s\n# %s\n\n", time.Now().Format(time.RFC3339Nano), strings.Join(shellQuote(full), " "))

	cmd := exec.Command(full[0], full[1:]...)
	cmd.Stdout = file
	cmd.Stderr = file
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = file.Close()
		return claudeTraceProbe{}, err
	}
	return claudeTraceProbe{Name: name, Cmd: cmd, File: file}, nil
}

func sudoNoPromptTraceArgv(argv []string) []string {
	if os.Geteuid() == 0 {
		return append([]string(nil), argv...)
	}
	full := []string{hostSudoPath, "-n"}
	return append(full, argv...)
}

func stopClaudeTraceProbes(probes []claudeTraceProbe) {
	for _, probe := range probes {
		stopClaudeTraceProbe(probe)
	}
}

func stopClaudeTraceProbe(probe claudeTraceProbe) {
	defer probe.File.Close()
	if probe.Cmd == nil || probe.Cmd.Process == nil {
		return
	}
	done := make(chan error, 1)
	go func() {
		done <- probe.Cmd.Wait()
	}()
	signalTraceProcessGroup(probe.Cmd, syscall.SIGINT)
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
	}
	signalTraceProcessGroup(probe.Cmd, syscall.SIGTERM)
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
	}
	signalTraceProcessGroup(probe.Cmd, syscall.SIGKILL)
	<-done
}

func signalTraceProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		_ = cmd.Process.Signal(sig)
	}
}

func startClaudeTraceProcessSampler(ctx context.Context, path string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return
		}
		defer file.Close()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sampleClaudeProcesses(file)
			}
		}
	}()
	return done
}

func sampleClaudeProcesses(w io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid,ppid,pgid,user,stat,etime,command").Output()
	if err != nil {
		fmt.Fprintf(w, "\n# %s ps failed: %v\n", time.Now().Format(time.RFC3339Nano), err)
		return
	}
	fmt.Fprintf(w, "\n# %s\n", time.Now().Format(time.RFC3339Nano))
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if processSampleLineRelevant(line) {
			fmt.Fprintln(w, line)
		}
	}
}

func processSampleLineRelevant(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "claude") ||
		strings.Contains(lower, "hazmat") ||
		strings.Contains(line, "/Users/agent/") ||
		strings.Contains(line, " agent ")
}

func writeTraceUnifiedLogs(dir string, start, end time.Time) {
	startArg := start.Add(-2 * time.Second).Format("2006-01-02 15:04:05")
	endArg := end.Add(2 * time.Second).Format("2006-01-02 15:04:05")
	predicate := `(process CONTAINS[c] "claude") || (process CONTAINS[c] "hazmat") || (process == "sandboxd") || (subsystem CONTAINS[c] "sandbox") || (eventMessage CONTAINS[c] "sandbox") || (eventMessage CONTAINS[c] "deny") || (eventMessage CONTAINS[c] "automation")`
	runTraceCommandToFile(filepath.Join(dir, "unified-log.json"), 90*time.Second,
		hostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", predicate)

	sandboxPredicate := `(process == "sandboxd") || (subsystem CONTAINS[c] "sandbox") || (eventMessage CONTAINS[c] "Sandbox:") || (eventMessage CONTAINS[c] "deny")`
	runTraceCommandToFile(filepath.Join(dir, "sandbox-log.json"), 90*time.Second,
		hostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", sandboxPredicate)
}

func writeTraceIndicators(dir string) {
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
	files := []string{
		"dtruss.log",
		"fs_usage.log",
		"opensnoop.log",
		"unified-log.json",
		"sandbox-log.json",
		"process-samples.log",
	}
	var b strings.Builder
	for _, file := range files {
		path := filepath.Join(dir, file)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "## %s\n", file)
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
	writeTraceText(dir, "indicators.md", b.String())
}

func writeTraceExperimentGuide(dir string) {
	content := `# Claude Trace Experiment Notes

Run each experiment with a short non-interactive Claude prompt first, then only
move to an interactive reproduction once the baseline is understood.

Suggested sequence:

1. Baseline: hazmat trace claude --name baseline -- --no-backup -p "say ok"
2. No network: hazmat trace claude --name network-none -- --no-backup --network none -p "say ok"
3. Docker Sandbox, if relevant: hazmat trace claude --name docker -- --no-backup --docker=sandbox -p "say ok"
4. Disable Claude's own permission bypass temporarily with
   hazmat config set session.skip_permissions false, rerun the baseline, then
   restore it with hazmat config set session.skip_permissions true.

Compare manifest.json, explain.json, sandbox-log.json, dtruss.log,
fs_usage.log, opensnoop.log, process-samples.log, and indicators.md across
runs. The strongest evidence is a probe or denial that appears only in failing
runs and disappears in the nearest passing control.
`
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
