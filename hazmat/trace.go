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
	opts := traceOptions{
		Harness:    spec.ID,
		Syscalls:   true,
		Transcript: true,
	}
	var noSyscalls bool
	var noTranscript bool

	cmd := &cobra.Command{
		Use:   string(spec.ID) + " [trace-flags] -- [hazmat-" + spec.CommandName + "-flags] [" + spec.CommandName + "-args...]",
		Short: "Trace a regular hazmat " + spec.CommandName + " launch",
		Long: fmt.Sprintf(`Trace a regular hazmat %s launch.

The command starts macOS observers before launching %s through the
same public Hazmat entrypoint used by `+"`hazmat %s`"+`. Results are written
to a timestamped directory under ~/.hazmat/traces unless --out is provided.

Put Hazmat/%s launch flags after -- so they are forwarded untouched.

Examples:
  hazmat trace %s -- %s
  hazmat trace %s --name baseline -- --no-backup %s
  hazmat trace %s --no-syscalls -- --network none %s`,
			spec.CommandName,
			spec.DisplayName,
			spec.CommandName,
			spec.DisplayName,
			spec.CommandName, strings.Join(shellQuote(spec.SampleArgs), " "),
			spec.CommandName, strings.Join(shellQuote(spec.SampleArgs), " "),
			spec.CommandName, strings.Join(shellQuote(spec.SampleArgs), " ")),
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if noSyscalls {
				opts.Syscalls = false
			}
			if noTranscript {
				opts.Transcript = false
			}
			return runHarnessTrace(opts, args)
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

func runHarnessTrace(opts traceOptions, forwarded []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("hazmat trace %s is currently implemented for macOS/Darwin only", opts.Harness)
	}

	spec, ok := traceHarnessSpecByID(opts.Harness)
	if !ok {
		return fmt.Errorf("unknown trace harness %q", opts.Harness)
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
	writeTraceExperimentGuide(traceDir, spec)
	writeTraceHarnessInfo(traceDir, spec)
	writeTraceHostSnapshot(traceDir, spec, "before")
	writeTraceExplain(traceDir, spec, forwarded)
	writeTraceToolProbe(traceDir, spec)

	ctx, cancel := context.WithCancel(context.Background())
	var samplerDone <-chan struct{}
	if opts.Syscalls {
		samplerDone = startTraceProcessSampler(ctx, filepath.Join(traceDir, "process-samples.log"), spec)
	}
	probes := startTraceProbes(opts.Syscalls, traceDir, spec)
	if opts.Syscalls {
		time.Sleep(750 * time.Millisecond)
	}

	launchErr := runTraceLaunch(traceDir, opts, launchArgs)
	end := time.Now()

	cancel()
	if samplerDone != nil {
		<-samplerDone
	}
	stopTraceProbes(probes)
	writeTraceHostSnapshot(traceDir, spec, "after")
	writeTraceUnifiedLogs(traceDir, start, end, spec)
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
	writeTraceManifest(traceDir, manifest)

	fmt.Fprintf(os.Stderr, "hazmat trace: bundle complete: %s\n", traceDir)
	return launchErr
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

func writeTraceToolProbe(dir string, spec traceHarnessSpec) {
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
		{Name: hostSudoPath, Args: []string{"-n", "-u", agentUser, "/usr/bin/env", "HOME=" + agentHome, "PATH=" + defaultAgentPath, "/usr/bin/which", spec.ProcessFilters[0]}},
	}
	for i, p := range probes {
		name := fmt.Sprintf("tool-probe-%02d-%s.txt", i+1, filepath.Base(p.Name))
		runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second, p.Name, p.Args...)
	}
}

func writeTraceHostSnapshot(dir string, spec traceHarnessSpec, phase string) {
	runTraceCommandToFile(filepath.Join(dir, phase+"-ps.txt"), 10*time.Second,
		"/bin/ps", "-axo", "pid,ppid,pgid,user,stat,etime,command")
	for _, path := range spec.AgentStatePaths {
		name := phase + "-agent-" + sanitizeTraceFilename(path) + "-ls.txt"
		runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second,
			hostSudoPath, "-n", "-u", agentUser, hostLsPath, "-laeO@", path)
	}
	for _, path := range spec.HostStatePaths {
		name := phase + "-host-" + sanitizeTraceFilename(path) + "-ls.txt"
		runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second,
			hostLsPath, "-laeO@", expandTilde(path))
	}
}

func sanitizeTraceFilename(path string) string {
	path = strings.TrimPrefix(path, "~/")
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, string(os.PathSeparator), "-")
	return sanitizeTraceLabel(path)
}

func runTraceLaunch(dir string, opts traceOptions, launchArgs []string) error {
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

type traceProbe struct {
	Name string
	Cmd  *exec.Cmd
	File *os.File
}

func startTraceProbes(enabled bool, dir string, spec traceHarnessSpec) []traceProbe {
	if !enabled {
		return nil
	}
	processFilter := spec.ProcessFilters[0]
	specs := []struct {
		name string
		file string
		argv []string
	}{
		{
			name: "dtruss",
			file: "dtruss.log",
			argv: []string{"/usr/bin/dtruss", "-f", "-a", "-d", "-e", "-l", "-W", processFilter},
		},
		{
			name: "fs_usage",
			file: "fs_usage.log",
			argv: []string{"/usr/bin/fs_usage", "-w", "-f", "filesys", processFilter},
		},
		{
			name: "opensnoop",
			file: "opensnoop.log",
			argv: []string{"/usr/bin/opensnoop", "-n", processFilter},
		},
	}

	var probes []traceProbe
	for _, probeSpec := range specs {
		probe, err := startTraceProbe(probeSpec.name, filepath.Join(dir, probeSpec.file), probeSpec.argv)
		if err != nil {
			appendTraceText(dir, "trace-errors.log", fmt.Sprintf("%s: %v\n", probeSpec.name, err))
			continue
		}
		probes = append(probes, probe)
	}
	return probes
}

func startTraceProbe(name, path string, argv []string) (traceProbe, error) {
	if len(argv) == 0 {
		return traceProbe{}, fmt.Errorf("missing argv")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return traceProbe{}, err
	}
	full := sudoNoPromptTraceArgv(argv)
	fmt.Fprintf(file, "# %s\n# %s\n\n", time.Now().Format(time.RFC3339Nano), strings.Join(shellQuote(full), " "))

	cmd := exec.Command(full[0], full[1:]...)
	cmd.Stdout = file
	cmd.Stderr = file
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = file.Close()
		return traceProbe{}, err
	}
	return traceProbe{Name: name, Cmd: cmd, File: file}, nil
}

func sudoNoPromptTraceArgv(argv []string) []string {
	if os.Geteuid() == 0 {
		return append([]string(nil), argv...)
	}
	full := []string{hostSudoPath, "-n"}
	return append(full, argv...)
}

func stopTraceProbes(probes []traceProbe) {
	for _, probe := range probes {
		stopTraceProbe(probe)
	}
}

func stopTraceProbe(probe traceProbe) {
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

func startTraceProcessSampler(ctx context.Context, path string, spec traceHarnessSpec) <-chan struct{} {
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
				sampleTraceProcesses(file, spec)
			}
		}
	}()
	return done
}

func sampleTraceProcesses(w io.Writer, spec traceHarnessSpec) {
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
		if processSampleLineRelevant(line, spec) {
			fmt.Fprintln(w, line)
		}
	}
}

func processSampleLineRelevant(line string, spec traceHarnessSpec) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "hazmat") ||
		strings.Contains(line, "/Users/agent/") ||
		strings.Contains(line, " agent ") {
		return true
	}
	for _, filter := range spec.ProcessFilters {
		if filter != "" && strings.Contains(lower, strings.ToLower(filter)) {
			return true
		}
	}
	return false
}

func writeTraceUnifiedLogs(dir string, start, end time.Time, spec traceHarnessSpec) {
	startArg := start.Add(-2 * time.Second).Format("2006-01-02 15:04:05")
	endArg := end.Add(2 * time.Second).Format("2006-01-02 15:04:05")
	predicate := traceUnifiedLogPredicate(spec)
	runTraceCommandToFile(filepath.Join(dir, "unified-log.json"), 90*time.Second,
		hostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", predicate)

	sandboxPredicate := `(process == "sandboxd") || (subsystem CONTAINS[c] "sandbox") || (eventMessage CONTAINS[c] "Sandbox:") || (eventMessage CONTAINS[c] "deny")`
	runTraceCommandToFile(filepath.Join(dir, "sandbox-log.json"), 90*time.Second,
		hostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", sandboxPredicate)
}

func traceUnifiedLogPredicate(spec traceHarnessSpec) string {
	terms := append([]string{"hazmat"}, spec.ProcessFilters...)
	parts := make([]string, 0, len(terms)+6)
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		term = strings.ReplaceAll(term, `"`, `\"`)
		parts = append(parts, fmt.Sprintf(`(process CONTAINS[c] "%s")`, term))
		parts = append(parts, fmt.Sprintf(`(eventMessage CONTAINS[c] "%s")`, term))
	}
	parts = append(parts,
		`(process == "sandboxd")`,
		`(subsystem CONTAINS[c] "sandbox")`,
		`(eventMessage CONTAINS[c] "sandbox")`,
		`(eventMessage CONTAINS[c] "deny")`,
		`(eventMessage CONTAINS[c] "automation")`,
	)
	return strings.Join(parts, " || ")
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

func writeTraceExperimentGuide(dir string, spec traceHarnessSpec) {
	sample := strings.Join(shellQuote(spec.SampleArgs), " ")
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

Compare manifest.json, explain.json, sandbox-log.json, dtruss.log,
fs_usage.log, opensnoop.log, process-samples.log, and indicators.md across
runs. The strongest evidence is a probe or denial that appears only in failing
runs and disappears in the nearest passing control.
`, spec.DisplayName, spec.CommandName, sample)
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
