package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type sessionGitSSHConfig struct {
	DisplayName    string
	PrivateKeyPath string
	KnownHostsPath string
	AllowedHosts   []string
	SessionNote    string
}

type preparedSessionRuntime struct {
	EnvPairs []string
	Cleanup  func()
}

type preparedSSHIdentityRuntime struct {
	SocketPath     string
	KnownHostsPath string
	Cleanup        func()
}

type provisionedSSHKey struct {
	Name           string
	PrivateKeyPath string
	PublicKeyPath  string
	KnownHostsPath string
	Fingerprint    string
	Status         string
}

func (k provisionedSSHKey) Usable() bool {
	return k.Status == "usable"
}

var gitSSHHostPattern = regexp.MustCompile(`^[a-z0-9.-]+$`)
var sshAgentPIDPattern = regexp.MustCompile(`SSH_AGENT_PID=([0-9]+);`)

func configuredProjectSSH(projectDir string) *ProjectSSHConfig {
	cfg, _ := loadConfig()
	return cfg.ProjectSSH(projectDir)
}

func configuredProjectGitSSH(projectDir string) *ProjectGitSSHConfig {
	cfg, _ := loadConfig()
	return cfg.ProjectGitSSH(projectDir)
}

func provisionedSSHKeysRootDir() string {
	return filepath.Join(filepath.Dir(configFilePath), "ssh", "keys")
}

func canonicalizeConfiguredFile(path string) (string, error) {
	path = strings.TrimSpace(expandTilde(path))
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	resolved, err := canonicalizePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a directory", resolved)
	}
	return resolved, nil
}

func normalizeGitSSHHosts(hosts []string) ([]string, error) {
	seen := make(map[string]struct{}, len(hosts))
	normalized := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		host := strings.ToLower(strings.TrimSpace(raw))
		if host == "" {
			continue
		}
		if !gitSSHHostPattern.MatchString(host) {
			return nil, fmt.Errorf("invalid host %q (expected bare hostname)", raw)
		}
		if _, dup := seen[host]; dup {
			continue
		}
		seen[host] = struct{}{}
		normalized = append(normalized, host)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("specify at least one --host")
	}
	return normalized, nil
}

func normalizeGitSSHTestHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("--host is required\nexample:\n  hazmat config ssh test -C ~/workspace/my-project --host github.com")
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if !gitSSHHostPattern.MatchString(host) {
		return "", fmt.Errorf("invalid host %q (expected bare hostname)", host)
	}
	return host, nil
}

func resolveManagedGitSSH(cfg sessionConfig) (*sessionGitSSHConfig, error) {
	if raw := configuredProjectSSH(cfg.ProjectDir); raw != nil {
		return resolveProvisionedManagedGitSSH(cfg, raw)
	}
	return resolveLegacyManagedGitSSH(cfg)
}

func resolveProvisionedManagedGitSSH(cfg sessionConfig, raw *ProjectSSHConfig) (*sessionGitSSHConfig, error) {
	keyName := strings.TrimSpace(raw.Key)
	if keyName == "" {
		return nil, fmt.Errorf("project ssh.key: value is required")
	}

	key, err := findProvisionedSSHKey(keyName)
	if err != nil {
		return nil, fmt.Errorf("project ssh.key: %w", err)
	}
	if !key.Usable() {
		return nil, fmt.Errorf("project ssh.key %q is not usable: %s", key.Name, key.Status)
	}
	if isWithinDir(agentHome, key.PrivateKeyPath) {
		return nil, fmt.Errorf("managed git ssh private key %q must stay outside %s", key.PrivateKeyPath, agentHome)
	}
	if sessionPathExposesFile(cfg, key.PrivateKeyPath) {
		return nil, fmt.Errorf("managed git ssh private key %q is visible inside the session contract; move it outside the project/read/write paths", key.PrivateKeyPath)
	}

	return &sessionGitSSHConfig{
		DisplayName:    key.Name,
		PrivateKeyPath: key.PrivateKeyPath,
		KnownHostsPath: key.KnownHostsPath,
		SessionNote: fmt.Sprintf(
			"Git-over-SSH enabled for this project via provisioned key %q. Hazmat keeps the private key in host-owned storage and loads it into a fresh session-local ssh-agent.",
			key.Name,
		),
	}, nil
}

func resolveLegacyManagedGitSSH(cfg sessionConfig) (*sessionGitSSHConfig, error) {
	raw := configuredProjectGitSSH(cfg.ProjectDir)
	if raw == nil {
		return nil, nil
	}

	privateKeyPath, err := canonicalizeConfiguredFile(raw.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("project git_ssh.private_key: %w", err)
	}
	if isWithinDir(agentHome, privateKeyPath) {
		return nil, fmt.Errorf("managed git ssh private key %q must stay outside %s", privateKeyPath, agentHome)
	}
	if sessionPathExposesFile(cfg, privateKeyPath) {
		return nil, fmt.Errorf("managed git ssh private key %q is visible inside the session contract; move it outside the project/read/write paths", privateKeyPath)
	}

	knownHostsPath, err := canonicalizeConfiguredFile(raw.KnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("project git_ssh.known_hosts: %w", err)
	}

	allowedHosts, err := normalizeGitSSHHosts(raw.AllowedHosts)
	if err != nil {
		return nil, fmt.Errorf("project git_ssh.allowed_hosts: %w", err)
	}

	return &sessionGitSSHConfig{
		DisplayName:    filepath.Base(privateKeyPath),
		PrivateKeyPath: privateKeyPath,
		KnownHostsPath: knownHostsPath,
		AllowedHosts:   allowedHosts,
		SessionNote: fmt.Sprintf(
			"Legacy host-scoped Git SSH enabled for hosts: %s. Hazmat keeps the private key in host-owned storage and loads it into a fresh session-local ssh-agent for Git only.",
			strings.Join(allowedHosts, ", "),
		),
	}, nil
}

func discoverProvisionedSSHKeys() ([]provisionedSSHKey, error) {
	root := provisionedSSHKeysRootDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read provisioned SSH keys in %s: %w", root, err)
	}

	keys := make([]provisionedSSHKey, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		keys = append(keys, inspectProvisionedSSHKey(entry.Name(), filepath.Join(root, entry.Name())))
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Name < keys[j].Name
	})
	return keys, nil
}

func inspectProvisionedSSHKey(name, dir string) provisionedSSHKey {
	key := provisionedSSHKey{Name: name}

	privateKeyPath, err := resolveProvisionedPrivateKeyPath(dir)
	if err != nil {
		key.Status = err.Error()
		return key
	}
	key.PrivateKeyPath = privateKeyPath

	knownHostsPath, err := canonicalizeConfiguredFile(filepath.Join(dir, "known_hosts"))
	if err != nil {
		key.Status = "missing known_hosts"
		return key
	}
	key.KnownHostsPath = knownHostsPath

	key.PublicKeyPath = resolveProvisionedPublicKeyPath(dir, privateKeyPath)
	key.Fingerprint = sshKeyFingerprint(key.PublicKeyPath)
	key.Status = "usable"
	return key
}

func resolveProvisionedPrivateKeyPath(dir string) (string, error) {
	overridePath := filepath.Join(dir, "private_key")
	if _, err := os.Stat(overridePath); err == nil {
		return canonicalizeConfiguredFile(overridePath)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read key directory: %w", err)
	}

	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "id_") || strings.HasSuffix(name, ".pub") {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, name))
	}
	sort.Strings(candidates)

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("missing private key")
	case 1:
		return canonicalizeConfiguredFile(candidates[0])
	default:
		return "", fmt.Errorf("multiple private key files found; add %s/private_key", dir)
	}
}

func resolveProvisionedPublicKeyPath(dir, privateKeyPath string) string {
	overridePath := filepath.Join(dir, "public_key")
	if path, err := canonicalizeConfiguredFile(overridePath); err == nil {
		return path
	}
	if path, err := canonicalizeConfiguredFile(privateKeyPath + ".pub"); err == nil {
		return path
	}
	return ""
}

func sshKeyFingerprint(publicKeyPath string) string {
	if publicKeyPath == "" {
		return ""
	}
	out, err := commandStdout("/usr/bin/ssh-keygen", "-lf", publicKeyPath)
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	if len(fields) >= 2 {
		return fields[1]
	}
	return strings.TrimSpace(out)
}

func usableProvisionedSSHKeys(keys []provisionedSSHKey) []provisionedSSHKey {
	usable := make([]provisionedSSHKey, 0, len(keys))
	for _, key := range keys {
		if key.Usable() {
			usable = append(usable, key)
		}
	}
	return usable
}

func findProvisionedSSHKey(name string) (provisionedSSHKey, error) {
	keys, err := discoverProvisionedSSHKeys()
	if err != nil {
		return provisionedSSHKey{}, err
	}
	return findProvisionedSSHKeyByName(keys, name)
}

func findProvisionedSSHKeyByName(keys []provisionedSSHKey, name string) (provisionedSSHKey, error) {
	name = strings.TrimSpace(name)
	for _, key := range keys {
		if key.Name == name {
			return key, nil
		}
	}
	return provisionedSSHKey{}, fmt.Errorf("SSH key %q was not found under %s", name, provisionedSSHKeysRootDir())
}

func sessionPathExposesFile(cfg sessionConfig, path string) bool {
	if isWithinDir(cfg.ProjectDir, path) {
		return true
	}
	for _, dir := range cfg.ReadDirs {
		if isWithinDir(dir, path) {
			return true
		}
	}
	for _, dir := range cfg.WriteDirs {
		if isWithinDir(dir, path) {
			return true
		}
	}
	return false
}

func prepareSessionRuntime(cfg sessionConfig) (preparedSessionRuntime, error) {
	runtime := preparedSessionRuntime{
		Cleanup: func() {},
	}
	if cfg.GitSSH == nil {
		return runtime, nil
	}

	gitSSHRuntime, err := prepareGitSSHRuntime(*cfg.GitSSH)
	if err != nil {
		return runtime, err
	}
	return gitSSHRuntime, nil
}

func prepareGitSSHRuntime(cfg sessionGitSSHConfig) (preparedSessionRuntime, error) {
	runtime := preparedSessionRuntime{
		Cleanup: func() {},
	}

	identityRuntime, err := prepareSSHIdentityRuntime(cfg)
	if err != nil {
		return runtime, err
	}

	wrapperDir := filepath.Dir(identityRuntime.SocketPath)
	wrapperPath := filepath.Join(wrapperDir, "git-ssh")
	wrapperScript := buildGitSSHWrapperScript(identityRuntime.SocketPath, identityRuntime.KnownHostsPath, cfg.AllowedHosts)
	if err := asAgentWriteFile(wrapperPath, []byte(wrapperScript), 0o700); err != nil {
		identityRuntime.Cleanup()
		return preparedSessionRuntime{Cleanup: func() {}}, fmt.Errorf("write managed git ssh wrapper: %w", err)
	}

	runtime.Cleanup = identityRuntime.Cleanup
	runtime.EnvPairs = []string{
		"GIT_SSH_COMMAND=" + wrapperPath,
		"GIT_SSH_VARIANT=ssh",
	}
	return runtime, nil
}

func prepareSSHIdentityRuntime(cfg sessionGitSSHConfig) (preparedSSHIdentityRuntime, error) {
	runtime := preparedSSHIdentityRuntime{
		Cleanup: func() {},
	}

	runtimeDir := filepath.Join(seatbeltProfileDir, "git-ssh", fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := asAgentMkdirAll(runtimeDir, 0o700); err != nil {
		return runtime, fmt.Errorf("prepare managed git ssh runtime dir: %w", err)
	}
	runtime.Cleanup = func() {
		_ = asAgentQuiet("rm", "-rf", runtimeDir)
	}

	socketPath := filepath.Join(runtimeDir, "agent.sock")
	pid, err := startAgentSSHAgent(socketPath)
	if err != nil {
		runtime.Cleanup()
		return preparedSSHIdentityRuntime{Cleanup: func() {}}, err
	}

	runtime.Cleanup = func() {
		cmd := newSudoCommand("-u", agentUser, "env",
			"SSH_AGENT_PID="+pid,
			"SSH_AUTH_SOCK="+socketPath,
			"/usr/bin/ssh-agent", "-k")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		_ = cmd.Run()
		_ = asAgentQuiet("rm", "-rf", runtimeDir)
	}

	if err := loadKeyIntoSSHAgent(socketPath, cfg.PrivateKeyPath); err != nil {
		runtime.Cleanup()
		return preparedSSHIdentityRuntime{Cleanup: func() {}}, err
	}

	knownHostsData, err := os.ReadFile(cfg.KnownHostsPath)
	if err != nil {
		runtime.Cleanup()
		return preparedSSHIdentityRuntime{Cleanup: func() {}}, fmt.Errorf("read managed git ssh known_hosts: %w", err)
	}
	runtimeKnownHostsPath := filepath.Join(runtimeDir, "known_hosts")
	if err := asAgentWriteFile(runtimeKnownHostsPath, knownHostsData, 0o600); err != nil {
		runtime.Cleanup()
		return preparedSSHIdentityRuntime{Cleanup: func() {}}, fmt.Errorf("write managed git ssh known_hosts: %w", err)
	}

	runtime.SocketPath = socketPath
	runtime.KnownHostsPath = runtimeKnownHostsPath
	return runtime, nil
}

func startAgentSSHAgent(socketPath string) (string, error) {
	cmd := newSudoCommand("-u", agentUser, "/usr/bin/ssh-agent", "-s", "-a", socketPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("start session ssh-agent: %s", msg)
	}
	pid, err := parseSSHAgentPID(stdout.String())
	if err != nil {
		return "", fmt.Errorf("start session ssh-agent: %w", err)
	}
	return pid, nil
}

func parseSSHAgentPID(output string) (string, error) {
	matches := sshAgentPIDPattern.FindStringSubmatch(output)
	if len(matches) != 2 {
		return "", fmt.Errorf("could not parse ssh-agent pid from output")
	}
	return matches[1], nil
}

func loadKeyIntoSSHAgent(socketPath, keyPath string) error {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read managed git ssh private key: %w", err)
	}

	cmd := newSudoCommand("-u", agentUser, "env",
		"SSH_AUTH_SOCK="+socketPath,
		"SSH_ASKPASS_REQUIRE=never",
		"/usr/bin/ssh-add", "-q", "-")
	cmd.Stdin = bytes.NewReader(keyData)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("load managed git ssh key into session agent: %s", msg)
	}
	return nil
}

func probeGitSSHHost(cfg sessionGitSSHConfig, host string) (string, error) {
	identityRuntime, err := prepareSSHIdentityRuntime(cfg)
	if err != nil {
		return "", err
	}
	defer identityRuntime.Cleanup()
	return runGitSSHProbe(identityRuntime.SocketPath, identityRuntime.KnownHostsPath, host)
}

func runGitSSHProbe(socketPath, knownHostsPath, host string) (string, error) {
	target := "git@" + host
	cmd := exec.Command("/usr/bin/ssh",
		"-F", "none",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "ForwardAgent=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "NumberOfPasswordPrompts=0",
		"-o", "RequestTTY=no",
		"-o", "IdentityAgent="+socketPath,
		"-o", "UserKnownHostsFile="+knownHostsPath,
		"-T",
		target,
		"git-upload-pack",
		"/__hazmat_ssh_probe__",
	)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if probeErr := interpretGitSSHProbeResult(host, trimmed, err); probeErr != nil {
		if trimmed == "" {
			return "", probeErr
		}
		return trimmed, probeErr
	}
	return trimmed, nil
}

func interpretGitSSHProbeResult(host, output string, err error) error {
	if err == nil {
		return nil
	}

	lower := strings.ToLower(output)
	for _, failure := range []string{
		"permission denied",
		"host key verification failed",
		"could not resolve hostname",
		"no route to host",
		"connection refused",
		"connection timed out",
		"operation timed out",
	} {
		if strings.Contains(lower, failure) {
			return fmt.Errorf("SSH test failed: %s", strings.TrimSpace(output))
		}
	}

	for _, success := range []string{
		"repository not found",
		"does not appear to be a git repository",
		"not a git repository",
		"project could not be found",
		"successfully authenticated",
		"welcome to gitlab",
	} {
		if strings.Contains(lower, success) {
			return nil
		}
	}

	return fmt.Errorf("SSH test failed for git@%s: %s", host, strings.TrimSpace(output))
}

func asAgentMkdirAll(path string, mode os.FileMode) error {
	cmd := newSudoCommand("-u", agentUser, "/usr/bin/install", "-d", "-m", fmt.Sprintf("%03o", mode.Perm()), path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func asAgentWriteFile(path string, content []byte, mode os.FileMode) error {
	cmd := newSudoCommand("-u", agentUser, "/usr/bin/tee", path)
	cmd.Stdin = bytes.NewReader(content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := asAgentQuiet("/bin/chmod", fmt.Sprintf("%03o", mode.Perm()), path); err != nil {
		return err
	}
	return nil
}

func buildGitSSHWrapperScript(socketPath, knownHostsPath string, allowedHosts []string) string {
	socketQuoted := shellQuote([]string{socketPath})[0]
	knownHostsQuoted := shellQuote([]string{knownHostsPath})[0]

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("set -eu\n\n")
	b.WriteString("reject() {\n")
	b.WriteString("  echo \"hazmat git-ssh: $*\" >&2\n")
	b.WriteString("  exit 97\n")
	b.WriteString("}\n\n")
	b.WriteString("port=\"\"\n")
	b.WriteString("while [ \"$#\" -gt 0 ]; do\n")
	b.WriteString("  case \"$1\" in\n")
	b.WriteString("    -4|-6|-T)\n")
	b.WriteString("      shift\n")
	b.WriteString("      ;;\n")
	b.WriteString("    -p)\n")
	b.WriteString("      [ \"$#\" -ge 2 ] || reject \"missing value for -p\"\n")
	b.WriteString("      port=\"$2\"\n")
	b.WriteString("      shift 2\n")
	b.WriteString("      ;;\n")
	b.WriteString("    -o)\n")
	b.WriteString("      [ \"$#\" -ge 2 ] || reject \"missing value for -o\"\n")
	b.WriteString("      case \"$2\" in\n")
	b.WriteString("        SendEnv=*|SetEnv=GIT_PROTOCOL=*) shift 2 ;;\n")
	b.WriteString("        *) reject \"unsupported ssh option: -o $2\" ;;\n")
	b.WriteString("      esac\n")
	b.WriteString("      ;;\n")
	b.WriteString("    -oSendEnv=*|-oSetEnv=GIT_PROTOCOL=*)\n")
	b.WriteString("      shift\n")
	b.WriteString("      ;;\n")
	b.WriteString("    --)\n")
	b.WriteString("      shift\n")
	b.WriteString("      break\n")
	b.WriteString("      ;;\n")
	b.WriteString("    -*)\n")
	b.WriteString("      reject \"unsupported ssh option: $1\"\n")
	b.WriteString("      ;;\n")
	b.WriteString("    *)\n")
	b.WriteString("      break\n")
	b.WriteString("      ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("done\n")
	b.WriteString("[ \"$#\" -ge 1 ] || reject \"missing destination host\"\n")
	b.WriteString("host=\"$1\"\n")
	b.WriteString("shift\n")
	if len(allowedHosts) > 0 {
		b.WriteString("normalized_host=\"$host\"\n")
		b.WriteString("case \"$normalized_host\" in\n")
		b.WriteString("  *@*) normalized_host=${normalized_host#*@} ;;\n")
		b.WriteString("esac\n")
		b.WriteString("case \"$normalized_host\" in\n")
		for _, host := range allowedHosts {
			fmt.Fprintf(&b, "  %s) ;;\n", host)
		}
		b.WriteString("  *) reject \"destination host not allowed: $normalized_host\" ;;\n")
		b.WriteString("esac\n")
	}
	b.WriteString("[ \"$#\" -gt 0 ] || reject \"interactive ssh is not allowed for this profile\"\n")
	b.WriteString("case \"$1\" in\n")
	b.WriteString("  git-upload-pack*|git-receive-pack*|git-upload-archive*) ;;\n")
	b.WriteString("  *) reject \"remote command not allowed: $1\" ;;\n")
	b.WriteString("esac\n")
	b.WriteString("if [ -n \"$port\" ]; then\n")
	b.WriteString("  set -- -p \"$port\" \"$host\" \"$@\"\n")
	b.WriteString("else\n")
	b.WriteString("  set -- \"$host\" \"$@\"\n")
	b.WriteString("fi\n")
	b.WriteString("exec /usr/bin/ssh \\\n")
	b.WriteString("  -F none \\\n")
	b.WriteString("  -o BatchMode=yes \\\n")
	b.WriteString("  -o IdentitiesOnly=yes \\\n")
	b.WriteString("  -o StrictHostKeyChecking=yes \\\n")
	fmt.Fprintf(&b, "  -o UserKnownHostsFile=%s \\\n", knownHostsQuoted)
	b.WriteString("  -o GlobalKnownHostsFile=/dev/null \\\n")
	b.WriteString("  -o ForwardAgent=no \\\n")
	b.WriteString("  -o ClearAllForwardings=yes \\\n")
	fmt.Fprintf(&b, "  -o IdentityAgent=%s \\\n", socketQuoted)
	b.WriteString("  \"$@\"\n")
	return b.String()
}
