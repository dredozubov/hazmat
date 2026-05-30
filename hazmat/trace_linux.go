//go:build linux

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
	"time"
)

type linuxTraceBackend struct{}

func currentTraceBackend() traceBackend {
	return linuxTraceBackend{}
}

func (linuxTraceBackend) name() string {
	return "linux"
}

func (linuxTraceBackend) supported() bool {
	return true
}

func (linuxTraceBackend) unsupportedError(HarnessID) error {
	return nil
}

func (linuxTraceBackend) observerDescription() string {
	return "Linux strace/proc observers"
}

func (linuxTraceBackend) syscallFlagHelp() string {
	return "Attempt Linux syscall tracing with strace and process/proc snapshots"
}

func (linuxTraceBackend) writeToolProbe(dir string, _ traceHarnessSpec) {
	runTraceCommandToFile(filepath.Join(dir, "tool-probe-01-uname.txt"), 10*time.Second, hostUnamePath, "-a")
	writeLinuxTraceFileSnapshot(filepath.Join(dir, "tool-probe-02-os-release.txt"), "/etc/os-release")
	writeLinuxTraceToolAvailability(filepath.Join(dir, "tool-probe-03-which.txt"),
		"strace", "ps", "journalctl", "dmesg", "ls", "stat")
	writeLinuxTraceFileSnapshot(filepath.Join(dir, "tool-probe-04-ptrace-scope.txt"), "/proc/sys/kernel/yama/ptrace_scope")
	writeLinuxCapabilityProbe(filepath.Join(dir, "tool-probe-05-capabilities.txt"))
}

func (linuxTraceBackend) writeHostSnapshot(dir string, spec traceHarnessSpec, phase string) {
	runLinuxTraceToolToFile(dir, phase+"-ps.txt", 10*time.Second,
		"ps", "-eo", "pid,ppid,pgid,user,stat,etime,args")
	writeLinuxTraceFileSnapshot(filepath.Join(dir, phase+"-proc-self-status.txt"), "/proc/self/status")
	writeLinuxMatchedProcStatus(filepath.Join(dir, phase+"-proc-process-status.txt"), spec)
	for _, path := range spec.AgentStatePaths {
		name := phase + "-agent-" + sanitizeTraceFilename(path) + "-ls.txt"
		runLinuxTraceToolToFile(dir, name, 10*time.Second, "ls", "-ld", "--", path)
	}
	for _, path := range spec.HostStatePaths {
		name := phase + "-host-" + sanitizeTraceFilename(path) + "-ls.txt"
		runLinuxTraceToolToFile(dir, name, 10*time.Second, "ls", "-ld", "--", expandTilde(path))
	}
}

func (linuxTraceBackend) startObservers(ctx context.Context, dir string, spec traceHarnessSpec, opts traceOptions) traceObserverSet {
	if !opts.Syscalls {
		return noopTraceObservers{}
	}
	return linuxTraceObservers{
		samplerDone: startLinuxTraceProcessSampler(ctx, filepath.Join(dir, "process-samples.log"), spec),
	}
}

func (linuxTraceBackend) runLaunch(dir string, opts traceOptions, launchArgs []string) error {
	cmd, err := newTraceLaunchCommand(dir, opts, launchArgs)
	if err != nil {
		return err
	}
	plan := planLinuxStrace(opts, resolveLinuxTraceTool)
	if !plan.Enabled {
		if plan.DegradedReason != "" {
			msg := fmt.Sprintf("# %s\n%s\n", time.Now().Format(time.RFC3339Nano), plan.DegradedReason)
			appendTraceText(dir, "trace-errors.log", msg)
			writeTraceText(dir, "strace.log", msg)
		}
		return runSessionCommand(cmd)
	}
	return runLinuxStraceLaunch(dir, plan.ToolPath, cmd)
}

func (linuxTraceBackend) writePostLaunchLogs(dir string, _ traceHarnessSpec, start, end time.Time) {
	since := start.Add(-2 * time.Second).Format(time.RFC3339)
	until := end.Add(2 * time.Second).Format(time.RFC3339)
	runLinuxTraceToolToFile(dir, "journal.log", 20*time.Second,
		"journalctl", "--no-pager", "--since", since, "--until", until)
	runLinuxTraceToolToFile(dir, "dmesg.log", 10*time.Second,
		"dmesg", "--ctime", "--color=never")
}

func (linuxTraceBackend) indicatorFiles() []string {
	return []string{
		"trace-errors.log",
		"strace-stderr.log",
		"strace.log",
		"strace.log.*",
		"process-samples.log",
		"before-ps.txt",
		"after-ps.txt",
		"journal.log",
		"dmesg.log",
		"tool-probe-*.txt",
		"before-proc-self-status.txt",
		"after-proc-self-status.txt",
		"before-proc-process-status.txt",
		"after-proc-process-status.txt",
	}
}

type linuxTraceObservers struct {
	samplerDone <-chan struct{}
}

func (o linuxTraceObservers) waitBeforeLaunch() {}

func (o linuxTraceObservers) stop() {
	if o.samplerDone != nil {
		<-o.samplerDone
	}
}

func runLinuxStraceLaunch(dir, stracePath string, cmd *exec.Cmd) error {
	target := append([]string{cmd.Path}, cmd.Args[1:]...)
	args := linuxStraceCommandArgs(filepath.Join(dir, "strace.log"), target)
	wrapped := exec.Command(stracePath, args...)
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = linuxTraceStderr(dir, cmd.Stderr)
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	err := runSessionCommand(wrapped)
	if closer, ok := wrapped.Stderr.(io.Closer); ok {
		_ = closer.Close()
	}
	if err != nil {
		appendTraceText(dir, "trace-errors.log", fmt.Sprintf("# %s\nstrace launch exited: %v\n", time.Now().Format(time.RFC3339Nano), err))
	}
	return err
}

type linuxTraceMultiWriteCloser struct {
	io.Writer
	close func() error
}

func (w linuxTraceMultiWriteCloser) Close() error {
	if w.close == nil {
		return nil
	}
	return w.close()
}

func linuxTraceStderr(dir string, primary io.Writer) io.Writer {
	if primary == nil {
		primary = os.Stderr
	}
	file, err := os.OpenFile(filepath.Join(dir, "strace-stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return primary
	}
	fmt.Fprintf(file, "# %s\n# strace stderr and traced child stderr\n\n", time.Now().Format(time.RFC3339Nano))
	return linuxTraceMultiWriteCloser{
		Writer: io.MultiWriter(primary, file),
		close:  file.Close,
	}
}

func startLinuxTraceProcessSampler(ctx context.Context, path string, spec traceHarnessSpec) <-chan struct{} {
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
				sampleLinuxTraceProcesses(file, spec)
			}
		}
	}()
	return done
}

func sampleLinuxTraceProcesses(w io.Writer, spec traceHarnessSpec) {
	lines, err := linuxTraceProcessLines(spec)
	if err != nil {
		fmt.Fprintf(w, "\n# %s ps failed: %v\n", time.Now().Format(time.RFC3339Nano), err)
		return
	}
	fmt.Fprintf(w, "\n# %s\n", time.Now().Format(time.RFC3339Nano))
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}

func linuxTraceProcessLines(spec traceHarnessSpec) ([]string, error) {
	psPath, ok := resolveLinuxTraceTool("ps")
	if !ok {
		return nil, fmt.Errorf("ps not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, psPath, "-eo", "pid,ppid,pgid,user,stat,etime,args").Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if processSampleLineRelevant(line, spec) {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func writeLinuxMatchedProcStatus(path string, spec traceHarnessSpec) {
	lines, err := linuxTraceProcessLines(spec)
	if err != nil {
		writeLinuxTracePath(path, fmt.Sprintf("# %s\n# collect matching process status: %v\n", time.Now().Format(time.RFC3339Nano), err))
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n# /proc/<pid>/status for matching ps rows\n\n", time.Now().Format(time.RFC3339Nano))
	if len(lines) == 0 {
		fmt.Fprintln(&b, "# no matching processes")
		writeLinuxTracePath(path, b.String())
		return
	}
	for _, line := range lines {
		pid, ok := linuxTracePIDFromPSLine(line)
		if !ok {
			continue
		}
		statusPath := filepath.Join("/proc", pid, "status")
		data, err := os.ReadFile(statusPath)
		fmt.Fprintf(&b, "## pid %s\n%s\n", pid, line)
		if err != nil {
			fmt.Fprintf(&b, "# read %s: %v\n\n", statusPath, err)
			continue
		}
		fmt.Fprintf(&b, "%s\n", data)
	}
	writeLinuxTracePath(path, b.String())
}

func linuxTracePIDFromPSLine(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	for _, r := range fields[0] {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return fields[0], true
}

func runLinuxTraceToolToFile(dir, name string, timeout time.Duration, tool string, args ...string) {
	path, ok := resolveLinuxTraceTool(tool)
	if !ok {
		writeTraceText(dir, name, fmt.Sprintf("# %s\n# %s not found in supported Linux tool paths\n", time.Now().Format(time.RFC3339Nano), tool))
		return
	}
	runTraceCommandToFile(filepath.Join(dir, name), timeout, path, args...)
}

func writeLinuxTraceToolAvailability(path string, tools ...string) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", time.Now().Format(time.RFC3339Nano))
	for _, tool := range tools {
		if path, ok := resolveLinuxTraceTool(tool); ok {
			fmt.Fprintf(&b, "%s: %s\n", tool, path)
		} else {
			fmt.Fprintf(&b, "%s: not found\n", tool)
		}
	}
	writeLinuxTracePath(path, b.String())
}

func writeLinuxTraceFileSnapshot(dest, source string) {
	data, err := os.ReadFile(source)
	if err != nil {
		writeLinuxTracePath(dest, fmt.Sprintf("# %s\n# read %s: %v\n", time.Now().Format(time.RFC3339Nano), source, err))
		return
	}
	writeLinuxTracePath(dest, fmt.Sprintf("# %s\n# %s\n\n%s", time.Now().Format(time.RFC3339Nano), source, data))
}

func writeLinuxCapabilityProbe(path string) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		writeLinuxTracePath(path, fmt.Sprintf("# %s\n# read /proc/self/status: %v\n", time.Now().Format(time.RFC3339Nano), err))
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n# /proc/self/status capability and confinement fields\n\n", time.Now().Format(time.RFC3339Nano))
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Cap"):
			fmt.Fprintln(&b, line)
		case strings.HasPrefix(line, "NoNewPrivs:"):
			fmt.Fprintln(&b, line)
		case strings.HasPrefix(line, "Seccomp:"):
			fmt.Fprintln(&b, line)
		case strings.HasPrefix(line, "Seccomp_filters:"):
			fmt.Fprintln(&b, line)
		}
	}
	writeLinuxTracePath(path, b.String())
}

func writeLinuxTracePath(path, content string) {
	_ = os.WriteFile(path, []byte(content), 0o600)
}

func resolveLinuxTraceTool(name string) (string, bool) {
	candidates := linuxTraceToolCandidates(name)
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, true
	}
	return "", false
}

func linuxTraceToolCandidates(name string) []string {
	switch name {
	case "strace":
		return []string{"/usr/bin/strace", "/bin/strace", "/usr/local/bin/strace"}
	case "ps":
		return []string{"/usr/bin/ps", "/bin/ps"}
	case "journalctl":
		return []string{"/usr/bin/journalctl", "/bin/journalctl"}
	case "dmesg":
		return []string{"/usr/bin/dmesg", "/bin/dmesg"}
	case "ls":
		return []string{hostLsPath, "/usr/bin/ls", "/bin/ls"}
	case "stat":
		return []string{"/usr/bin/stat", "/bin/stat"}
	default:
		return nil
	}
}
