//go:build hazmat_debug && darwin

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type darwinTraceBackend struct{}

func currentTraceBackend() traceBackend {
	return darwinTraceBackend{}
}

func (darwinTraceBackend) name() string {
	return "darwin"
}

func (darwinTraceBackend) supported() bool {
	return true
}

func (darwinTraceBackend) unsupportedError(HarnessID) error {
	return nil
}

func (darwinTraceBackend) observerDescription() string {
	return "macOS observers"
}

func (darwinTraceBackend) preflight(_ traceHarnessSpec, opts traceOptions) error {
	required := map[string]string{
		"sudo":      hostSudoPath,
		"uname":     hostUnamePath,
		"sw_vers":   "/usr/bin/sw_vers",
		"csrutil":   "/usr/bin/csrutil",
		"which":     "/usr/bin/which",
		"ps":        "/bin/ps",
		"ls":        hostLsPath,
		"script":    hostScriptPath,
		"log":       hostLogPath,
		"dtruss":    "/usr/bin/dtruss",
		"fs_usage":  "/usr/bin/fs_usage",
		"opensnoop": "/usr/bin/opensnoop",
	}
	for label, path := range required {
		if err := requireTraceExecutable(label, path); err != nil {
			return err
		}
	}
	if err := runTracePreflightCommand(10*time.Second, hostSudoPath, "-n", "-v"); err != nil {
		return fmt.Errorf("trace requires non-interactive sudo before launch: %w", err)
	}
	if opts.Syscalls {
		if err := runDarwinDTracePreflight(); err != nil {
			return fmt.Errorf("trace requires working dtruss/DTrace before launch: %w", err)
		}
	}
	return nil
}

func (darwinTraceBackend) writeToolProbe(dir string, spec traceHarnessSpec) {
	type probe struct {
		Name string   `json:"name"`
		Args []string `json:"args"`
	}
	self, err := os.Executable()
	dtrussProbeArgs := []string{"-n", "/usr/bin/dtruss", "/usr/bin/true"}
	if err == nil {
		dtrussProbeArgs = []string{"-n", "/usr/bin/dtruss", self, "--version"}
	}
	probes := []probe{
		{Name: hostUnamePath, Args: []string{"-a"}},
		{Name: "/usr/bin/sw_vers"},
		{Name: "/usr/bin/csrutil", Args: []string{"status"}},
		{Name: "/usr/bin/which", Args: []string{"dtruss", "fs_usage", "opensnoop", "execsnoop", "sample", "spindump", "script", "log"}},
		{Name: hostSudoPath, Args: []string{"-n", "-v"}},
		{Name: hostSudoPath, Args: dtrussProbeArgs},
		{Name: hostSudoPath, Args: []string{"-n", "-u", agentUser, "/usr/bin/env", "HOME=" + agentHome, "PATH=" + defaultAgentPath, "/usr/bin/which", spec.ProcessFilters[0]}},
	}
	for i, p := range probes {
		name := fmt.Sprintf("tool-probe-%02d-%s.txt", i+1, filepath.Base(p.Name))
		runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second, p.Name, p.Args...)
	}
}

func runDarwinDTracePreflight() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable for DTrace probe: %w", err)
	}
	return runTracePreflightCommand(10*time.Second, hostSudoPath, "-n", "/usr/bin/dtruss", self, "--version")
}

func (darwinTraceBackend) writeHostSnapshot(dir string, spec traceHarnessSpec, phase string) {
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

func (darwinTraceBackend) startObservers(ctx context.Context, dir string, spec traceHarnessSpec, opts traceOptions) (traceObserverSet, error) {
	if !opts.Syscalls {
		return noopTraceObservers{}, nil
	}
	probes, err := startDarwinTraceProbes(dir, spec)
	if err != nil {
		return nil, err
	}
	return darwinTraceObservers{
		samplerDone: startDarwinTraceProcessSampler(ctx, filepath.Join(dir, "process-samples.log"), spec),
		probes:      probes,
		warmup:      750 * time.Millisecond,
	}, nil
}

func (darwinTraceBackend) runLaunch(dir string, opts traceOptions, launchArgs []string) error {
	return runTraceLaunch(dir, opts, launchArgs)
}

func traceScriptCommandArgs(transcript, self string, launchArgs []string) []string {
	return append([]string{"-q", transcript, self}, launchArgs...)
}

func (darwinTraceBackend) writePostLaunchLogs(dir string, spec traceHarnessSpec, start, end time.Time) {
	writeDarwinTraceUnifiedLogs(dir, start, end, spec)
}

func (darwinTraceBackend) indicatorFiles() []string {
	return []string{
		"dtruss.log",
		"fs_usage.log",
		"opensnoop.log",
		"unified-log.json",
		"sandbox-log.json",
		"process-samples.log",
	}
}

type darwinTraceObservers struct {
	samplerDone <-chan struct{}
	probes      []darwinTraceProbe
	warmup      time.Duration
}

func (o darwinTraceObservers) waitBeforeLaunch() {
	if o.warmup > 0 {
		time.Sleep(o.warmup)
	}
}

func (o darwinTraceObservers) stop() {
	if o.samplerDone != nil {
		<-o.samplerDone
	}
	stopDarwinTraceProbes(o.probes)
}

type darwinTraceProbe struct {
	Name string
	Cmd  *exec.Cmd
	File *os.File
}

func startDarwinTraceProbes(dir string, spec traceHarnessSpec) ([]darwinTraceProbe, error) {
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

	var probes []darwinTraceProbe
	for _, probeSpec := range specs {
		probe, err := startDarwinTraceProbe(probeSpec.name, filepath.Join(dir, probeSpec.file), probeSpec.argv)
		if err != nil {
			stopDarwinTraceProbes(probes)
			return nil, fmt.Errorf("start %s trace probe: %w", probeSpec.name, err)
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

func startDarwinTraceProbe(name, path string, argv []string) (darwinTraceProbe, error) {
	if len(argv) == 0 {
		return darwinTraceProbe{}, fmt.Errorf("missing argv")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return darwinTraceProbe{}, err
	}
	full := sudoNoPromptTraceArgv(argv)
	fmt.Fprintf(file, "# %s\n# %s\n\n", time.Now().Format(time.RFC3339Nano), strings.Join(shellQuote(full), " "))

	cmd := exec.Command(full[0], full[1:]...)
	cmd.Stdout = file
	cmd.Stderr = file
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = file.Close()
		return darwinTraceProbe{}, err
	}
	return darwinTraceProbe{Name: name, Cmd: cmd, File: file}, nil
}

func sudoNoPromptTraceArgv(argv []string) []string {
	if os.Geteuid() == 0 {
		return append([]string(nil), argv...)
	}
	full := []string{hostSudoPath, "-n"}
	return append(full, argv...)
}

func stopDarwinTraceProbes(probes []darwinTraceProbe) {
	for _, probe := range probes {
		stopDarwinTraceProbe(probe)
	}
}

func stopDarwinTraceProbe(probe darwinTraceProbe) {
	defer probe.File.Close()
	if probe.Cmd == nil || probe.Cmd.Process == nil {
		return
	}
	done := make(chan error, 1)
	go func() {
		done <- probe.Cmd.Wait()
	}()
	signalDarwinTraceProcessGroup(probe.Cmd, syscall.SIGINT)
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
	}
	signalDarwinTraceProcessGroup(probe.Cmd, syscall.SIGTERM)
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
	}
	signalDarwinTraceProcessGroup(probe.Cmd, syscall.SIGKILL)
	<-done
}

func signalDarwinTraceProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		_ = cmd.Process.Signal(sig)
	}
}

func startDarwinTraceProcessSampler(ctx context.Context, path string, spec traceHarnessSpec) <-chan struct{} {
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
				sampleDarwinTraceProcesses(file, spec)
			}
		}
	}()
	return done
}

func sampleDarwinTraceProcesses(w io.Writer, spec traceHarnessSpec) {
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

func writeDarwinTraceUnifiedLogs(dir string, start, end time.Time, spec traceHarnessSpec) {
	startArg := start.Add(-2 * time.Second).Format("2006-01-02 15:04:05")
	endArg := end.Add(2 * time.Second).Format("2006-01-02 15:04:05")
	predicate := darwinTraceUnifiedLogPredicate(spec)
	runTraceCommandToFile(filepath.Join(dir, "unified-log.json"), 90*time.Second,
		hostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", predicate)

	sandboxPredicate := `(process == "sandboxd") || (subsystem CONTAINS[c] "sandbox") || (eventMessage CONTAINS[c] "Sandbox:") || (eventMessage CONTAINS[c] "deny")`
	runTraceCommandToFile(filepath.Join(dir, "sandbox-log.json"), 90*time.Second,
		hostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", sandboxPredicate)
}

func darwinTraceUnifiedLogPredicate(spec traceHarnessSpec) string {
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
