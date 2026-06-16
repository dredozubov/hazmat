package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	PackagePath = "hazmat/internal/runtime/docker"
	BackendType = "docker-sandboxes"
)

var ErrSandboxNotFound = errors.New("sandbox not found")

type Probe interface {
	Output(name string, args ...string) (string, error)
	Run(name string, args ...string) (string, error)
}

type PolicyProfile struct {
	Name       string
	Policy     string
	AllowHosts []string
}

type LaunchSpec struct {
	Name           string
	Agent          string
	ProjectDir     string
	Profile        PolicyProfile
	MountReadDirs  []string
	MountWriteDirs []string
}

type ManagedSandbox struct {
	Name string
}

type Backend struct {
	stderr io.Writer
}

func NewBackend(stderr io.Writer) Backend {
	if stderr == nil {
		stderr = os.Stderr
	}
	return Backend{stderr: stderr}
}

func (Backend) Type() string {
	return BackendType
}

func (b Backend) PrepareLaunch(probe Probe, spec LaunchSpec) error {
	status, exists, err := Status(probe, spec.Name)
	if err != nil {
		return err
	}
	if exists {
		if status != "" && status != "running" {
			fmt.Fprintf(b.stderr, "hazmat: removing stopped Docker Sandbox %s\n", spec.Name)
			out, rmErr := probe.Output("docker", "sandbox", "rm", spec.Name)
			if rmErr != nil && !Missing(out) {
				return fmt.Errorf("remove stopped Docker Sandbox %s: %s", spec.Name, OneLine(out))
			}
			exists = false
		}
	}
	if exists {
		fmt.Fprintf(b.stderr, "hazmat: reusing Docker Sandbox %s\n", spec.Name)
	} else {
		fmt.Fprintf(b.stderr, "hazmat: creating Docker Sandbox %s (first launch may take a few minutes)\n", spec.Name)
		args := []string{"sandbox", "create", "--name", spec.Name, spec.Agent, spec.ProjectDir}
		args = append(args, spec.MountWriteDirs...)
		for _, dir := range spec.MountReadDirs {
			args = append(args, dir+":ro")
		}
		if out, err := probe.Run("docker", args...); err != nil {
			return actionError(probe, spec.Agent, out, err, "create Docker Sandbox %s", spec.Name)
		}
	}

	fmt.Fprintf(b.stderr, "hazmat: applying Docker network policy to %s\n", spec.Name)
	policyArgs := []string{"sandbox", "network", "proxy", spec.Name, "--policy", spec.Profile.Policy}
	for _, host := range spec.Profile.AllowHosts {
		policyArgs = append(policyArgs, "--allow-host", host)
	}
	if out, err := probe.Run("docker", policyArgs...); err != nil {
		return actionError(probe, spec.Agent, out, err, "apply Docker network policy to %s", spec.Name)
	}
	return nil
}

func (Backend) RunAgentSession(probe Probe, agent, sandboxName string, forwarded []string) error {
	args := []string{"sandbox", "run", sandboxName}
	if len(forwarded) > 0 {
		args = append(args, "--")
		args = append(args, forwarded...)
	}
	if out, err := probe.Run("docker", args...); err != nil {
		return actionError(probe, agent, out, err, "run %s in Docker Sandbox %s", AgentDisplayName(agent), sandboxName)
	}
	return nil
}

func (Backend) RunShellSession(probe Probe, sandboxName, projectDir string) error {
	if out, err := probe.Run("docker", "sandbox", "run", sandboxName, "--",
		"-lc", `cd "$1" && exec /bin/bash -il`, "bash", projectDir); err != nil {
		return actionError(probe, "shell", out, err, "run shell in Docker Sandbox %s", sandboxName)
	}
	return nil
}

func (Backend) RunExecSession(probe Probe, sandboxName, projectDir string, commandArgs []string) error {
	args := []string{"sandbox", "run", sandboxName, "--",
		"-lc", `cd "$1" && shift && exec "$@"`, "bash", projectDir}
	args = append(args, commandArgs...)
	if out, err := probe.Run("docker", args...); err != nil {
		return actionError(probe, "shell", out, err, "run exec session in Docker Sandbox %s", sandboxName)
	}
	return nil
}

func (Backend) RemoveManagedSandboxes(probe Probe, sandboxes []ManagedSandbox) error {
	for _, sandbox := range sandboxes {
		out, err := probe.Output("docker", "sandbox", "rm", sandbox.Name)
		if err != nil {
			if Missing(out) {
				continue
			}
			return fmt.Errorf("remove Docker Sandbox %s: %s", sandbox.Name, OneLine(out))
		}
	}
	return nil
}

func Status(probe Probe, name string) (string, bool, error) {
	out, err := probe.Output("docker", "sandbox", "ls", "--json")
	if err != nil {
		return "", false, fmt.Errorf("list Docker Sandboxes: %s", OneLine(out))
	}
	sandboxes, err := ParseSandboxList(out)
	if err != nil {
		return "", false, err
	}
	for _, sandbox := range sandboxes {
		if sandbox.Name == name {
			return strings.ToLower(sandbox.Status), true, nil
		}
	}
	return "", false, nil
}

type Sandbox struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

func ParseSandboxList(raw string) ([]Sandbox, error) {
	type sandboxListResponse struct {
		VMs       []Sandbox `json:"vms"`
		Sandboxes []Sandbox `json:"sandboxes"`
	}
	var parsed sandboxListResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("docker sandbox ls --json did not return valid JSON: %w", err)
	}
	switch {
	case strings.Contains(raw, `"sandboxes"`):
		return parsed.Sandboxes, nil
	case strings.Contains(raw, `"vms"`):
		return parsed.VMs, nil
	default:
		return nil, fmt.Errorf(`docker sandbox ls --json did not include a "sandboxes" or "vms" field`)
	}
}

func AgentDisplayName(agent string) string {
	switch agent {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "gemini":
		return "Gemini"
	case "hermes":
		return "Hermes"
	case "qwen":
		return "Qwen Code"
	case "cursor-agent":
		return "Cursor Agent"
	case "pi":
		return "Pi"
	case "shell":
		return "shell"
	default:
		if strings.TrimSpace(agent) == "" {
			return "agent"
		}
		return agent
	}
}

func Missing(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no such")
}

func OneLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no output"
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[0])
}

func actionError(probe Probe, agent, output string, err error, format string, args ...any) error {
	base := fmt.Sprintf(format, args...)
	if output != "" {
		if sandboxNotFoundError(output) {
			return fmt.Errorf("%s: %w", base, ErrSandboxNotFound)
		}
		if hint, ok := sandboxAuthErrorHint(agent, output); ok {
			return fmt.Errorf("%s: %s", base, hint)
		}
	}
	if dockerDesktopClosedPipeError(output, err) {
		return fmt.Errorf("%s: Docker Desktop failed unexpectedly; if macOS showed a Docker data-access prompt, click Allow and retry; otherwise restart Docker Desktop and retry", base)
	}
	status, statusErr := dockerDesktopStatus(probe)
	if statusErr == nil && status == "stopped" {
		return fmt.Errorf("%s: Docker Desktop stopped unexpectedly; restart Docker Desktop and retry", base)
	}
	return fmt.Errorf("%s", base)
}

func dockerDesktopStatus(probe Probe) (string, error) {
	out, err := probe.Output("docker", "desktop", "status")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[0] == "Status" {
			return strings.ToLower(fields[1]), nil
		}
	}
	return "", fmt.Errorf("status line not found")
}

func sandboxAuthErrorHint(agent, output string) (string, bool) {
	switch agent {
	case "claude":
		if claudeSandboxAuthError(output) {
			return "Claude is not authenticated in Docker Sandboxes; run 'hazmat claude' interactively and type /login, or configure ANTHROPIC_API_KEY in your shell startup files and restart Docker Desktop", true
		}
	}
	return "", false
}

func sandboxNotFoundError(output string) bool {
	return strings.Contains(strings.ToLower(output), "no sandbox found")
}

func claudeSandboxAuthError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "not logged in") && strings.Contains(lower, "/login")
}

func dockerDesktopClosedPipeError(output string, err error) bool {
	var text strings.Builder
	text.WriteString(strings.ToLower(output))
	if err != nil {
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(strings.ToLower(err.Error()))
	}
	return strings.Contains(text.String(), "io: read/write on closed pipe")
}
