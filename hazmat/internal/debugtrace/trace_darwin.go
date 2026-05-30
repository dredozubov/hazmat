//go:build hazmat_debug && darwin

package debugtrace

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

func (darwinTraceBackend) preflight(env Env, _ HarnessSpec, opts Options) error {
	required := map[string]string{
		"sudo":      env.HostSudoPath,
		"uname":     env.HostUnamePath,
		"sw_vers":   "/usr/bin/sw_vers",
		"csrutil":   "/usr/bin/csrutil",
		"which":     "/usr/bin/which",
		"ps":        "/bin/ps",
		"ls":        env.HostLsPath,
		"script":    env.HostScriptPath,
		"log":       env.HostLogPath,
		"dtruss":    "/usr/bin/dtruss",
		"fs_usage":  "/usr/bin/fs_usage",
		"opensnoop": "/usr/bin/opensnoop",
	}
	for label, path := range required {
		if err := requireTraceExecutable(label, path); err != nil {
			return err
		}
	}
	if err := runTracePreflightCommand(10*time.Second, env.HostSudoPath, "-n", "-v"); err != nil {
		return fmt.Errorf("trace requires non-interactive sudo before launch: %w", err)
	}
	if opts.Syscalls {
		if err := runTracePreflightCommand(10*time.Second, env.HostSudoPath, "-n", "/usr/bin/dtruss", "/usr/bin/true"); err != nil {
			return fmt.Errorf("trace requires working dtruss/DTrace before launch: %w", err)
		}
	}
	return nil
}

func (darwinTraceBackend) writeToolProbe(env Env, dir string, spec HarnessSpec) error {
	type probe struct {
		Name string   `json:"name"`
		Args []string `json:"args"`
	}
	probes := []probe{
		{Name: env.HostUnamePath, Args: []string{"-a"}},
		{Name: "/usr/bin/sw_vers"},
		{Name: "/usr/bin/csrutil", Args: []string{"status"}},
		{Name: "/usr/bin/which", Args: []string{"dtruss", "fs_usage", "opensnoop", "script", "log"}},
		{Name: env.HostSudoPath, Args: []string{"-n", "-v"}},
		{Name: env.HostSudoPath, Args: []string{"-n", "/usr/bin/dtruss", "/usr/bin/true"}},
		{Name: env.HostSudoPath, Args: []string{"-n", "-u", env.AgentUser, "/usr/bin/env", "HOME=" + env.AgentHome, "PATH=" + env.DefaultAgentPath, "/usr/bin/which", spec.ProcessFilters[0]}},
	}
	for i, p := range probes {
		name := fmt.Sprintf("tool-probe-%02d-%s.txt", i+1, filepath.Base(p.Name))
		if err := runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second, p.Name, p.Args...); err != nil {
			return err
		}
	}
	return nil
}

func (darwinTraceBackend) writeHostSnapshot(env Env, dir string, spec HarnessSpec, phase string) error {
	if err := runRequiredTraceCommandToFile(filepath.Join(dir, phase+"-ps.txt"), 10*time.Second,
		"/bin/ps", "-axo", "pid,ppid,pgid,user,stat,etime,command"); err != nil {
		return err
	}
	for _, path := range spec.AgentStatePaths {
		name := phase + "-agent-" + sanitizeTraceFilename(path) + "-ls.txt"
		if err := runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second,
			env.HostSudoPath, "-n", "-u", env.AgentUser, env.HostLsPath, "-laeO@", path); err != nil {
			return err
		}
	}
	for _, path := range spec.HostStatePaths {
		name := phase + "-host-" + sanitizeTraceFilename(path) + "-ls.txt"
		if env.ExpandTilde != nil {
			path = env.ExpandTilde(path)
		}
		if err := runTraceCommandToFile(filepath.Join(dir, name), 10*time.Second,
			env.HostLsPath, "-laeO@", path); err != nil {
			return err
		}
	}
	return nil
}

func (darwinTraceBackend) startObservers(ctx context.Context, env Env, dir string, spec HarnessSpec, opts Options) (traceObserverSet, error) {
	if !opts.Syscalls {
		return nil, fmt.Errorf("trace requires macOS syscall observers")
	}
	probes, err := startDarwinTraceProbes(env, dir, spec)
	if err != nil {
		return nil, err
	}
	samplerDone, err := startDarwinTraceProcessSampler(ctx, env, filepath.Join(dir, "process-samples.log"), spec)
	if err != nil {
		stopDarwinTraceProbes(probes)
		return nil, err
	}
	return darwinTraceObservers{
		samplerDone: samplerDone,
		probes:      probes,
		warmup:      750 * time.Millisecond,
	}, nil
}

func (darwinTraceBackend) runLaunch(env Env, dir string, opts Options, launchArgs []string) error {
	return runTraceLaunch(env, dir, opts, launchArgs)
}

func traceScriptCommandArgs(transcript, self string, launchArgs []string) []string {
	return append([]string{"-q", transcript, self}, launchArgs...)
}

func (darwinTraceBackend) writePostLaunchLogs(env Env, dir string, spec HarnessSpec, start, end time.Time) error {
	return writeDarwinTraceUnifiedLogs(env, dir, start, end, spec)
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

func startDarwinTraceProbes(env Env, dir string, spec HarnessSpec) ([]darwinTraceProbe, error) {
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
		probe, err := startDarwinTraceProbe(env, probeSpec.name, filepath.Join(dir, probeSpec.file), probeSpec.argv)
		if err != nil {
			stopDarwinTraceProbes(probes)
			return nil, fmt.Errorf("start %s trace probe: %w", probeSpec.name, err)
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

func startDarwinTraceProbe(env Env, name, path string, argv []string) (darwinTraceProbe, error) {
	if len(argv) == 0 {
		return darwinTraceProbe{}, fmt.Errorf("missing argv")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return darwinTraceProbe{}, err
	}
	full := sudoNoPromptTraceArgv(env, argv)
	fmt.Fprintf(file, "# %s\n# %s\n\n", time.Now().Format(time.RFC3339Nano), strings.Join(ShellQuote(full), " "))

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

func sudoNoPromptTraceArgv(env Env, argv []string) []string {
	if os.Geteuid() == 0 {
		return append([]string(nil), argv...)
	}
	full := []string{env.HostSudoPath, "-n"}
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

func startDarwinTraceProcessSampler(ctx context.Context, env Env, path string, spec HarnessSpec) (<-chan struct{}, error) {
	done := make(chan struct{})
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create process samples log: %w", err)
	}
	go func() {
		defer close(done)
		defer file.Close()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sampleDarwinTraceProcesses(env, file, spec)
			}
		}
	}()
	return done, nil
}

func sampleDarwinTraceProcesses(env Env, w io.Writer, spec HarnessSpec) {
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
		if processSampleLineRelevant(env, line, spec) {
			fmt.Fprintln(w, line)
		}
	}
}

func writeDarwinTraceUnifiedLogs(env Env, dir string, start, end time.Time, spec HarnessSpec) error {
	startArg := start.Add(-2 * time.Second).Format("2006-01-02 15:04:05")
	endArg := end.Add(2 * time.Second).Format("2006-01-02 15:04:05")
	predicate := darwinTraceUnifiedLogPredicate(spec)
	if err := runRequiredTraceCommandToFile(filepath.Join(dir, "unified-log.json"), 90*time.Second,
		env.HostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", predicate); err != nil {
		return err
	}

	sandboxPredicate := `(process == "sandboxd") || (subsystem CONTAINS[c] "sandbox") || (eventMessage CONTAINS[c] "Sandbox:") || (eventMessage CONTAINS[c] "deny")`
	if err := runRequiredTraceCommandToFile(filepath.Join(dir, "sandbox-log.json"), 90*time.Second,
		env.HostLogPath, "show", "--style", "json", "--start", startArg, "--end", endArg, "--predicate", sandboxPredicate); err != nil {
		return err
	}
	return nil
}

func darwinTraceUnifiedLogPredicate(spec HarnessSpec) string {
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
