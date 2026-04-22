package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type sessionGitSSHConfig struct {
	DisplayName string
	SessionNote string
	Keys        []sessionGitSSHKey
}

// sessionGitSSHKey is one resolved identity within a session Git-SSH config.
// AllowedHosts may be empty for a profile-backed key whose profile has no
// default_hosts and whose project key declares no override; such a key matches
// no destination host.
type sessionGitSSHKey struct {
	Name           string
	PrivateKeyPath string
	KnownHostsPath string
	AllowedHosts   []string
}

// PrimaryKey returns the first configured key, or nil when the config has no
// keys. Legacy callers that only care about a single key can use this shortcut
// instead of indexing; multi-key aware callers should iterate Keys directly.
func (c *sessionGitSSHConfig) PrimaryKey() *sessionGitSSHKey {
	if c == nil || len(c.Keys) == 0 {
		return nil
	}
	return &c.Keys[0]
}

type preparedSessionRuntime struct {
	EnvPairs []string
	Cleanup  func()
}

type preparedSSHIdentityRuntime struct {
	Cleanup func()
	Keys    []preparedSSHIdentityKey
}

type preparedSSHIdentityKey struct {
	Name           string
	SocketPath     string
	KnownHostsPath string
	AllowedHosts   []string
}

type sshKeyCandidate struct {
	DirectoryPath  string
	PrivateKeyPath string
	PublicKeyPath  string
	KnownHostsPath string
	Fingerprint    string
	Status         string
}

func (k sshKeyCandidate) Usable() bool {
	return k.Status == "usable"
}

func (k sshKeyCandidate) DisplayName() string {
	if k.PrivateKeyPath == "" {
		return "(missing private key)"
	}
	return filepath.Base(k.PrivateKeyPath)
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
var gitSSHPortPattern = regexp.MustCompile(`^[0-9]+$`)
var sshAgentPIDPattern = regexp.MustCompile(`SSH_AGENT_PID=([0-9]+);`)

type gitSSHTestTarget struct {
	RequestedHost         string
	InputUser             string
	InputPort             string
	Host                  string
	User                  string
	Port                  string
	HostKeyAlias          string
	ResolvedFromSSHConfig bool
	JumpTargets           []gitSSHJumpTarget
}

type gitSSHJumpTarget struct {
	Host string
	User string
	Port string
}

func formatGitSSHAuthority(host, user, defaultUser string) string {
	if user == "" {
		user = defaultUser
	}
	summary := host
	if user != "" {
		summary = user + "@" + summary
	}
	return summary
}

func formatGitSSHEndpoint(host, user, port, defaultUser string) string {
	summary := formatGitSSHAuthority(host, user, defaultUser)
	if port != "" {
		summary += ":" + port
	}
	return summary
}

func (t gitSSHTestTarget) resolutionSummary() string {
	return formatGitSSHEndpoint(t.Host, t.User, t.Port, "git")
}

func (t gitSSHJumpTarget) summary() string {
	return formatGitSSHEndpoint(t.Host, t.User, t.Port, "")
}

type sshConfigAliasResolution struct {
	HostName             string
	User                 string
	Port                 string
	ProxyJump            string
	HostKeyAlias         string
	UnsupportedDirective string
}

func provisionedSSHKeysRootDir() string {
	return filepath.Join(filepath.Dir(configFilePath), "ssh", "keys")
}

func defaultSSHKeyDirectory() string {
	return filepath.Join(os.Getenv("HOME"), ".ssh")
}

func resolveSSHKeyDirectory(dir string) (string, error) {
	dir = strings.TrimSpace(expandTilde(dir))
	if dir == "" {
		dir = defaultSSHKeyDirectory()
	}
	return resolveDir(dir, false)
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
	target, err := parseGitSSHTestTarget(host)
	if err != nil {
		return "", err
	}
	return target.Host, nil
}

func parseGitSSHTestTarget(host string) (gitSSHTestTarget, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return gitSSHTestTarget{}, fmt.Errorf("--host is required\nexample:\n  hazmat config ssh test -C ~/workspace/my-project --host github.com")
	}

	target := gitSSHTestTarget{}

	switch {
	case strings.Contains(host, "://"):
		parsed, err := url.Parse(host)
		if err != nil {
			return gitSSHTestTarget{}, fmt.Errorf("invalid host %q", host)
		}
		if parsed.Hostname() == "" {
			return gitSSHTestTarget{}, fmt.Errorf("invalid host %q", host)
		}
		target.Host = parsed.Hostname()
		target.User = parsed.User.Username()
		target.InputUser = target.User
		target.Port = parsed.Port()
		target.InputPort = target.Port
	default:
		if colon := strings.Index(host, ":"); colon >= 0 {
			suffix := host[colon+1:]
			slash := strings.Index(host, "/")
			if (slash == -1 || colon < slash) && gitSSHPortPattern.MatchString(suffix) {
				return gitSSHTestTarget{}, fmt.Errorf("invalid host %q (use ssh://host:%s or configure Port in ~/.ssh/config)", host, suffix)
			}
		}
		if colon := strings.Index(host, ":"); colon >= 0 {
			slash := strings.Index(host, "/")
			if slash == -1 || colon < slash {
				host = host[:colon]
			}
		}
		if at := strings.LastIndex(host, "@"); at >= 0 {
			target.User = host[:at]
			target.InputUser = target.User
			host = host[at+1:]
		}
		target.Host = host
	}

	target.Host = strings.ToLower(strings.TrimSpace(target.Host))
	if !gitSSHHostPattern.MatchString(target.Host) {
		return gitSSHTestTarget{}, fmt.Errorf("invalid host %q (expected bare hostname)", target.Host)
	}
	if target.Port != "" && !gitSSHPortPattern.MatchString(target.Port) {
		return gitSSHTestTarget{}, fmt.Errorf("invalid port %q", target.Port)
	}
	target.RequestedHost = target.Host
	return target, nil
}

func resolveGitSSHTestTarget(host string) (gitSSHTestTarget, error) {
	target, err := parseGitSSHTestTarget(host)
	if err != nil {
		return gitSSHTestTarget{}, err
	}
	return resolveGitSSHTestTargetWithSeen(target, make(map[string]struct{}))
}

func resolveGitSSHTestTargetWithSeen(target gitSSHTestTarget, seen map[string]struct{}) (gitSSHTestTarget, error) {
	if _, ok := seen[target.RequestedHost]; ok {
		return gitSSHTestTarget{}, fmt.Errorf("ssh config host resolution cycle detected for %q", target.RequestedHost)
	}
	seen[target.RequestedHost] = struct{}{}
	defer delete(seen, target.RequestedHost)

	resolved, err := resolveSSHConfigAlias(target.Host)
	if err != nil {
		return gitSSHTestTarget{}, err
	}
	if target.User == "" && resolved.User != "" {
		target.User = resolved.User
		target.ResolvedFromSSHConfig = true
	}
	if target.Port == "" && resolved.Port != "" {
		target.Port = resolved.Port
		target.ResolvedFromSSHConfig = true
	}
	if resolved.HostName != "" {
		normalizedHost := strings.ToLower(strings.TrimSpace(resolved.HostName))
		if !gitSSHHostPattern.MatchString(normalizedHost) {
			return gitSSHTestTarget{}, fmt.Errorf("ssh config host %q resolved to invalid HostName %q", target.RequestedHost, resolved.HostName)
		}
		if normalizedHost != target.Host {
			target.ResolvedFromSSHConfig = true
		}
		target.Host = normalizedHost
	}
	if resolved.HostKeyAlias != "" {
		target.HostKeyAlias = resolved.HostKeyAlias
		target.ResolvedFromSSHConfig = true
	} else if target.Host != target.RequestedHost {
		target.HostKeyAlias = target.RequestedHost
	}
	if resolved.ProxyJump != "" && !strings.EqualFold(strings.TrimSpace(resolved.ProxyJump), "none") {
		jumpTargets, err := resolveSSHConfigProxyJump(resolved.ProxyJump, seen)
		if err != nil {
			return gitSSHTestTarget{}, err
		}
		target.JumpTargets = jumpTargets
		target.ResolvedFromSSHConfig = true
	}

	return target, nil
}

func resolveSSHConfigAlias(host string) (sshConfigAliasResolution, error) {
	configPath := filepath.Join(defaultSSHKeyDirectory(), "config")
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sshConfigAliasResolution{}, nil
		}
		return sshConfigAliasResolution{}, fmt.Errorf("read ssh config %s: %w", configPath, err)
	}

	resolution := sshConfigAliasResolution{}
	visited := make(map[string]struct{})
	if err := applySSHConfigFile(host, configPath, &resolution, visited); err != nil {
		return sshConfigAliasResolution{}, err
	}
	return resolution, nil
}

func applySSHConfigFile(host, configPath string, resolution *sshConfigAliasResolution, visited map[string]struct{}) error {
	configPath = expandTilde(configPath)
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Clean(configPath)
	}
	canonicalPath, err := canonicalizePath(configPath)
	if err != nil {
		return fmt.Errorf("ssh config %s: %w", configPath, err)
	}
	if _, seen := visited[canonicalPath]; seen {
		return nil
	}
	visited[canonicalPath] = struct{}{}

	file, err := os.Open(canonicalPath)
	if err != nil {
		return fmt.Errorf("open ssh config %s: %w", canonicalPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	active := true
	for scanner.Scan() {
		line := stripSSHConfigComment(scanner.Text())
		if line == "" {
			continue
		}
		fields := splitSSHConfigFields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		values := fields[1:]

		switch key {
		case "host":
			active = sshConfigHostMatches(host, values)
		case "match":
			active = false
		case "include":
			if !active {
				continue
			}
			for _, pattern := range values {
				includePaths, err := resolveSSHConfigIncludePaths(pattern, filepath.Dir(canonicalPath))
				if err != nil {
					return err
				}
				for _, includePath := range includePaths {
					if err := applySSHConfigFile(host, includePath, resolution, visited); err != nil {
						return err
					}
				}
			}
		case "hostname":
			if active && resolution.HostName == "" {
				resolution.HostName = values[0]
			}
		case "user":
			if active && resolution.User == "" {
				resolution.User = values[0]
			}
		case "port":
			if active && resolution.Port == "" {
				if !gitSSHPortPattern.MatchString(values[0]) {
					return fmt.Errorf("ssh config host %q has invalid Port %q", host, values[0])
				}
				resolution.Port = values[0]
			}
		case "proxyjump":
			if active && resolution.ProxyJump == "" {
				resolution.ProxyJump = values[0]
			}
		case "hostkeyalias":
			if active && resolution.HostKeyAlias == "" {
				resolution.HostKeyAlias = values[0]
			}
		case "proxycommand":
			if active && resolution.UnsupportedDirective == "" {
				resolution.UnsupportedDirective = key
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan ssh config %s: %w", canonicalPath, err)
	}
	return nil
}

func stripSSHConfigComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}

func splitSSHConfigFields(line string) []string {
	fields := strings.Fields(line)
	for i := range fields {
		fields[i] = strings.Trim(fields[i], `"'`)
	}
	return fields
}

func sshConfigHostMatches(host string, patterns []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	matched := false
	for _, raw := range patterns {
		pattern := strings.ToLower(strings.TrimSpace(raw))
		if pattern == "" {
			continue
		}
		negated := strings.HasPrefix(pattern, "!")
		if negated {
			pattern = strings.TrimPrefix(pattern, "!")
		}
		ok, err := path.Match(pattern, host)
		if err != nil {
			continue
		}
		if !ok {
			continue
		}
		if negated {
			return false
		}
		matched = true
	}
	return matched
}

func resolveSSHConfigIncludePaths(pattern, baseDir string) ([]string, error) {
	pattern = expandTilde(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil, nil
	}
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(baseDir, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("ssh config Include %q: %w", pattern, err)
	}
	sort.Strings(matches)
	return matches, nil
}

func parseSSHConfigJumpTarget(value string) (gitSSHJumpTarget, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return gitSSHJumpTarget{}, fmt.Errorf("empty ProxyJump entry")
	}

	target := gitSSHJumpTarget{}
	if at := strings.LastIndex(value, "@"); at >= 0 {
		target.User = value[:at]
		value = value[at+1:]
	}
	if colon := strings.LastIndex(value, ":"); colon >= 0 {
		port := value[colon+1:]
		if gitSSHPortPattern.MatchString(port) {
			target.Port = port
			value = value[:colon]
		}
	}

	target.Host = strings.ToLower(strings.TrimSpace(value))
	if !gitSSHHostPattern.MatchString(target.Host) {
		return gitSSHJumpTarget{}, fmt.Errorf("invalid ProxyJump host %q", value)
	}
	return target, nil
}

func resolveSSHConfigProxyJump(value string, seen map[string]struct{}) ([]gitSSHJumpTarget, error) {
	parts := strings.Split(value, ",")
	targets := make([]gitSSHJumpTarget, 0, len(parts))
	for _, part := range parts {
		target, err := parseSSHConfigJumpTarget(part)
		if err != nil {
			return nil, err
		}

		resolvedTarget, err := resolveGitSSHTestTargetWithSeen(gitSSHTestTarget{
			RequestedHost: target.Host,
			Host:          target.Host,
			User:          target.User,
			Port:          target.Port,
		}, seen)
		if err != nil {
			return nil, err
		}
		targets = append(targets, resolvedTarget.JumpTargets...)
		targets = append(targets, gitSSHJumpTarget{
			Host: resolvedTarget.Host,
			User: resolvedTarget.User,
			Port: resolvedTarget.Port,
		})
	}
	return targets, nil
}

func resolveManagedGitSSH(cfg sessionConfig) (*sessionGitSSHConfig, error) {
	fullCfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if raw := fullCfg.ProjectSSH(cfg.ProjectDir); raw != nil {
		return resolveProjectSSHKeys(cfg, raw, fullCfg.SSHProfiles)
	}
	if raw := fullCfg.ProjectGitSSH(cfg.ProjectDir); raw != nil {
		return resolveLegacyManagedGitSSH(cfg, raw)
	}
	return nil, nil
}

// resolveProjectSSHKeys walks the normalized key list for a project SSH
// config and produces a sessionGitSSHConfig with one sessionGitSSHKey per
// entry. Each key's identity resolves through a profile reference, an
// inline PrivateKeyPath, or the provisioned inventory; visibility and
// containment checks run per-key so a single stray identity blocks the
// whole session.
func resolveProjectSSHKeys(cfg sessionConfig, raw *ProjectSSHConfig, profiles map[string]SSHProfile) (*sessionGitSSHConfig, error) {
	if err := ValidateProjectSSHConfig(*raw); err != nil {
		return nil, fmt.Errorf("project ssh: %w", err)
	}
	if err := ValidateProjectSSHProfileRefs(*raw, profiles); err != nil {
		return nil, fmt.Errorf("project ssh: %w", err)
	}
	entries := raw.NormalizedKeys()
	if len(entries) == 0 {
		return nil, nil
	}

	resolved := make([]sessionGitSSHKey, 0, len(entries))
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		key, err := resolveProjectSSHKeyEntry(cfg, entry, profiles)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, key)
		names = append(names, key.Name)
	}

	displayName := names[0]
	if len(names) > 1 {
		displayName = strings.Join(names, "+")
	}
	return &sessionGitSSHConfig{
		DisplayName: displayName,
		SessionNote: sessionNoteForKeys(resolved),
		Keys:        resolved,
	}, nil
}

func resolveProjectSSHKeyEntry(cfg sessionConfig, entry ProjectSSHKey, profiles map[string]SSHProfile) (sessionGitSSHKey, error) {
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = "default"
	}

	privateKeyPath, knownHostsPath, err := resolveProjectSSHKeyIdentity(entry, profiles)
	if err != nil {
		return sessionGitSSHKey{}, fmt.Errorf("ssh key %q: %w", name, err)
	}
	if isWithinDir(agentHome, privateKeyPath) {
		return sessionGitSSHKey{}, fmt.Errorf("ssh key %q: private key %q must stay outside %s", name, privateKeyPath, agentHome)
	}
	if sessionPathExposesFile(cfg, privateKeyPath) {
		return sessionGitSSHKey{}, fmt.Errorf("ssh key %q: private key %q is visible inside the session contract; move it outside the project/read/write paths", name, privateKeyPath)
	}

	// Declared Hosts take precedence; otherwise inherit the referenced
	// profile's DefaultHosts. A profile-backed key without either ends up
	// with no allowed hosts and routes nothing.
	hostInput := entry.Hosts
	if len(hostInput) == 0 && strings.TrimSpace(entry.Profile) != "" {
		if profile, ok := profiles[strings.TrimSpace(entry.Profile)]; ok {
			hostInput = profile.DefaultHosts
		}
	}
	allowedHosts, err := normalizeGitSSHHosts(hostInput)
	if err != nil && len(hostInput) > 0 {
		return sessionGitSSHKey{}, fmt.Errorf("ssh key %q: %w", name, err)
	}
	if len(hostInput) == 0 {
		allowedHosts = nil
	}

	return sessionGitSSHKey{
		Name:           name,
		PrivateKeyPath: privateKeyPath,
		KnownHostsPath: knownHostsPath,
		AllowedHosts:   allowedHosts,
	}, nil
}

func resolveProjectSSHKeyIdentity(entry ProjectSSHKey, profiles map[string]SSHProfile) (string, string, error) {
	profileName := strings.TrimSpace(entry.Profile)
	if profileName != "" {
		profile, ok := profiles[profileName]
		if !ok {
			return "", "", fmt.Errorf("profile %q is not defined in ssh_profiles", profileName)
		}
		privateKeyPath, err := canonicalizeConfiguredFile(profile.PrivateKeyPath)
		if err != nil {
			return "", "", fmt.Errorf("profile %q private_key: %w", profileName, err)
		}
		knownHostsRaw := profile.KnownHostsPath
		if strings.TrimSpace(knownHostsRaw) == "" {
			knownHostsRaw = filepath.Join(filepath.Dir(privateKeyPath), "known_hosts")
		}
		knownHostsPath, err := canonicalizeConfiguredFile(knownHostsRaw)
		if err != nil {
			return "", "", fmt.Errorf("profile %q known_hosts: %w", profileName, err)
		}
		return privateKeyPath, knownHostsPath, nil
	}

	if strings.TrimSpace(entry.PrivateKeyPath) != "" {
		privateKeyPath, err := canonicalizeConfiguredFile(entry.PrivateKeyPath)
		if err != nil {
			return "", "", fmt.Errorf("private_key: %w", err)
		}
		knownHostsRaw := entry.KnownHostsPath
		if strings.TrimSpace(knownHostsRaw) == "" {
			knownHostsRaw = filepath.Join(filepath.Dir(privateKeyPath), "known_hosts")
		}
		knownHostsPath, err := canonicalizeConfiguredFile(knownHostsRaw)
		if err != nil {
			return "", "", fmt.Errorf("known_hosts: %w", err)
		}
		return privateKeyPath, knownHostsPath, nil
	}

	keyName := strings.TrimSpace(entry.Key)
	if keyName == "" {
		return "", "", fmt.Errorf("profile, private_key, or key is required")
	}
	provisioned, err := findProvisionedSSHKey(keyName)
	if err != nil {
		return "", "", fmt.Errorf("key: %w", err)
	}
	if !provisioned.Usable() {
		return "", "", fmt.Errorf("key %q is not usable: %s", provisioned.Name, provisioned.Status)
	}
	return provisioned.PrivateKeyPath, provisioned.KnownHostsPath, nil
}

func sessionNoteForKeys(keys []sessionGitSSHKey) string {
	if len(keys) == 1 {
		key := keys[0]
		if len(key.AllowedHosts) == 0 {
			return fmt.Sprintf(
				"Git-over-SSH configured for this project via selected key %q, but it has no effective hosts. Hazmat will reject every destination until hosts are declared or inherited.",
				key.Name,
			)
		}
		return fmt.Sprintf(
			"Git-over-SSH enabled for this project via selected key %q (hosts: %s). Hazmat keeps the private key in host-owned storage and loads it into a fresh session-local ssh-agent.",
			key.Name,
			strings.Join(key.AllowedHosts, ", "),
		)
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		hosts := strings.Join(key.AllowedHosts, ", ")
		if hosts == "" {
			hosts = "(no hosts)"
		}
		parts = append(parts, fmt.Sprintf("%s → %s", key.Name, hosts))
	}
	return fmt.Sprintf(
		"Git-over-SSH enabled for this project with per-host key routing: %s. Hazmat keeps the private keys in host-owned storage and loads each into its own session-local ssh-agent.",
		strings.Join(parts, "; "),
	)
}

func resolveLegacyManagedGitSSH(cfg sessionConfig, raw *ProjectGitSSHConfig) (*sessionGitSSHConfig, error) {
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

	displayName := filepath.Base(privateKeyPath)
	return &sessionGitSSHConfig{
		DisplayName: displayName,
		SessionNote: fmt.Sprintf(
			"Legacy host-scoped Git SSH enabled for hosts: %s. Hazmat keeps the private key in host-owned storage and loads it into a fresh session-local ssh-agent for Git only.",
			strings.Join(allowedHosts, ", "),
		),
		Keys: []sessionGitSSHKey{{
			Name:           displayName,
			PrivateKeyPath: privateKeyPath,
			KnownHostsPath: knownHostsPath,
			AllowedHosts:   allowedHosts,
		}},
	}, nil
}

func discoverSSHKeyCandidates(dir string) ([]sshKeyCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read SSH keys in %s: %w", dir, err)
	}

	keys := make([]sshKeyCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !looksLikeSSHPrivateKeyCandidate(name) {
			continue
		}
		keys = append(keys, inspectSSHKeyCandidate(filepath.Join(dir, name)))
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].DisplayName() < keys[j].DisplayName()
	})
	return keys, nil
}

func looksLikeSSHPrivateKeyCandidate(name string) bool {
	if strings.HasSuffix(name, ".pub") {
		return false
	}
	switch name {
	case "known_hosts", "known_hosts.old", "config", "authorized_keys":
		return false
	}
	return !strings.HasPrefix(name, ".")
}

func inspectSSHKeyCandidate(path string) sshKeyCandidate {
	key := sshKeyCandidate{}

	privateKeyPath, err := canonicalizeConfiguredFile(path)
	if err != nil {
		key.Status = err.Error()
		return key
	}
	key.PrivateKeyPath = privateKeyPath
	key.DirectoryPath = filepath.Dir(privateKeyPath)

	knownHostsPath, err := canonicalizeConfiguredFile(filepath.Join(key.DirectoryPath, "known_hosts"))
	if err != nil {
		key.Status = "missing known_hosts"
	} else {
		key.KnownHostsPath = knownHostsPath
		key.Status = "usable"
	}

	key.PublicKeyPath = resolveConfiguredPublicKeyPath(privateKeyPath)
	key.Fingerprint = sshKeyFingerprint(key.PublicKeyPath)
	return key
}

func resolveConfiguredPublicKeyPath(privateKeyPath string) string {
	if path, err := canonicalizeConfiguredFile(privateKeyPath + ".pub"); err == nil {
		return path
	}
	return ""
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

func usableSSHKeyCandidates(keys []sshKeyCandidate) []sshKeyCandidate {
	usable := make([]sshKeyCandidate, 0, len(keys))
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

func findSSHKeyCandidate(keys []sshKeyCandidate, selection string) (sshKeyCandidate, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return sshKeyCandidate{}, fmt.Errorf("SSH key selection is required")
	}
	if looksLikeSSHPublicKeySelection(selection) {
		return sshKeyCandidate{}, fmt.Errorf("SSH key %q looks like a public key; pass the private key path instead", selection)
	}

	if strings.Contains(selection, string(os.PathSeparator)) || filepath.IsAbs(selection) {
		canonical, err := canonicalizeConfiguredFile(selection)
		if err == nil {
			for _, key := range keys {
				if key.PrivateKeyPath == canonical {
					return key, nil
				}
			}
		}
	}

	for _, key := range keys {
		if key.DisplayName() == selection || key.PrivateKeyPath == selection {
			return key, nil
		}
	}
	if len(keys) == 0 {
		return sshKeyCandidate{}, fmt.Errorf("SSH key %q was not found", selection)
	}
	return sshKeyCandidate{}, fmt.Errorf("SSH key %q was not found in %s", selection, keys[0].DirectoryPath)
}

func looksLikeSSHPublicKeySelection(selection string) bool {
	return strings.HasSuffix(strings.TrimSpace(selection), ".pub")
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
	if len(cfg.Keys) == 0 {
		return runtime, nil
	}

	identityRuntime, err := prepareSSHIdentityRuntime(cfg)
	if err != nil {
		return runtime, err
	}

	wrapperDir := identityRuntime.runtimeDir()
	wrapperPath := filepath.Join(wrapperDir, "git-ssh")
	wrapperScript := buildGitSSHWrapperScript(identityRuntime.Keys)
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

// runtimeDir recovers the runtime directory containing all per-key sockets
// and known_hosts files. Every preparedSSHIdentityKey lives under the same
// directory by construction, so we return the parent of the first socket.
func (r preparedSSHIdentityRuntime) runtimeDir() string {
	if len(r.Keys) == 0 {
		return ""
	}
	return filepath.Dir(r.Keys[0].SocketPath)
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

	agentTeardowns := make([]func(), 0, len(cfg.Keys))
	appendTeardown := func(fn func()) {
		agentTeardowns = append(agentTeardowns, fn)
	}
	rollback := func() {
		for i := len(agentTeardowns) - 1; i >= 0; i-- {
			agentTeardowns[i]()
		}
		_ = asAgentQuiet("rm", "-rf", runtimeDir)
	}

	// Derive socket paths from validated key names, then assert pairwise
	// distinctness at session time. Names are already unique at config-set
	// time (ValidateProjectSSHConfig), so this is a defensive check for the
	// TLA invariant SocketsDistinctForPresent.
	seenSocket := make(map[string]string, len(cfg.Keys))
	preparedKeys := make([]preparedSSHIdentityKey, 0, len(cfg.Keys))

	for _, key := range cfg.Keys {
		socketPath := filepath.Join(runtimeDir, "agent-"+key.Name+".sock")
		if prior, dup := seenSocket[socketPath]; dup {
			rollback()
			return preparedSSHIdentityRuntime{Cleanup: func() {}}, fmt.Errorf("ssh keys %q and %q resolve to the same session socket %q", prior, key.Name, socketPath)
		}
		seenSocket[socketPath] = key.Name

		pid, err := startAgentSSHAgent(socketPath)
		if err != nil {
			rollback()
			return preparedSSHIdentityRuntime{Cleanup: func() {}}, err
		}
		killSocket := socketPath
		killPID := pid
		appendTeardown(func() {
			cmd := newAgentCommand("env",
				"SSH_AGENT_PID="+killPID,
				"SSH_AUTH_SOCK="+killSocket,
				"/usr/bin/ssh-agent", "-k")
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			_ = cmd.Run()
		})

		if err := loadKeyIntoSSHAgent(socketPath, key.PrivateKeyPath); err != nil {
			rollback()
			return preparedSSHIdentityRuntime{Cleanup: func() {}}, fmt.Errorf("ssh key %q: %w", key.Name, err)
		}

		knownHostsData, err := os.ReadFile(key.KnownHostsPath)
		if err != nil {
			rollback()
			return preparedSSHIdentityRuntime{Cleanup: func() {}}, fmt.Errorf("ssh key %q: read known_hosts: %w", key.Name, err)
		}
		runtimeKnownHostsPath := filepath.Join(runtimeDir, "known_hosts-"+key.Name)
		if err := asAgentWriteFile(runtimeKnownHostsPath, knownHostsData, 0o600); err != nil {
			rollback()
			return preparedSSHIdentityRuntime{Cleanup: func() {}}, fmt.Errorf("ssh key %q: write known_hosts: %w", key.Name, err)
		}

		preparedKeys = append(preparedKeys, preparedSSHIdentityKey{
			Name:           key.Name,
			SocketPath:     socketPath,
			KnownHostsPath: runtimeKnownHostsPath,
			AllowedHosts:   append([]string(nil), key.AllowedHosts...),
		})
	}

	runtime.Cleanup = rollback
	runtime.Keys = preparedKeys
	return runtime, nil
}

func startAgentSSHAgent(socketPath string) (string, error) {
	cmd := newAgentCommand("/usr/bin/ssh-agent", "-s", "-a", socketPath)
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

	cmd := newAgentCommand("env",
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

func probeGitSSHHost(key sessionGitSSHKey, target gitSSHTestTarget) (string, error) {
	return runGitSSHProbe(key.PrivateKeyPath, key.KnownHostsPath, target)
}

// selectSessionGitSSHKey picks the session key that matches the destination
// host per the TLA MC_GitSSHRouting contract: exactly one match for a ready
// config, or a reject when nothing matches. Keys with no effective
// AllowedHosts match nothing.
func selectSessionGitSSHKey(cfg *sessionGitSSHConfig, host string) (*sessionGitSSHKey, error) {
	if cfg == nil || len(cfg.Keys) == 0 {
		return nil, fmt.Errorf("no SSH keys configured")
	}
	normalized := strings.ToLower(strings.TrimSpace(host))
	var matches []*sessionGitSSHKey
	for i := range cfg.Keys {
		key := &cfg.Keys[i]
		for _, pattern := range key.AllowedHosts {
			if ok, _ := filepath.Match(pattern, normalized); ok {
				matches = append(matches, key)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no SSH key configured for host %q", host)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, key := range matches {
			names = append(names, key.Name)
		}
		return nil, fmt.Errorf("host %q matches multiple SSH keys: %s", host, strings.Join(names, ", "))
	}
}

func newGitSSHProbeCommand(privateKeyPath, knownHostsPath string, target gitSSHTestTarget) *exec.Cmd {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + knownHostsPath,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "ForwardAgent=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "NumberOfPasswordPrompts=0",
		"-o", "RequestTTY=no",
		"-i", privateKeyPath,
	}
	if target.InputUser != "" {
		args = append(args, "-l", target.InputUser)
	}
	if target.InputPort != "" {
		args = append(args, "-p", target.InputPort)
	}
	args = append(args,
		"-T",
		target.RequestedHost,
		"git-upload-pack",
		"/__hazmat_ssh_probe__",
	)
	return exec.Command("/usr/bin/ssh", args...)
}

func runGitSSHProbe(privateKeyPath, knownHostsPath string, target gitSSHTestTarget) (string, error) {
	cmd := newGitSSHProbeCommand(privateKeyPath, knownHostsPath, target)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if probeErr := interpretGitSSHProbeResult(target.RequestedHost, trimmed, err); probeErr != nil {
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
	return agentEnsureDir(path, mode)
}

func asAgentWriteFile(path string, content []byte, mode os.FileMode) error {
	return agentWriteFile(path, content, mode)
}

// buildGitSSHWrapperScript renders the shell wrapper that routes a Git-over-SSH
// invocation to exactly one prepared key's identity-agent socket.
//
// The routing contract matches tla/MC_GitSSHRouting.tla:
//   - The destination host must match exactly one key's AllowedHosts patterns
//     (shell glob semantics via `case`); anything else is rejected.
//   - Prepared keys with no effective AllowedHosts are omitted from the case
//     arms and therefore match no destination host.
//
// The wrapper passes only protocol handshake options to ssh and rejects
// interactive shells or unknown remote commands.
func buildGitSSHWrapperScript(keys []preparedSSHIdentityKey) string {
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
	b.WriteString("normalized_host=\"$host\"\n")
	b.WriteString("case \"$normalized_host\" in\n")
	b.WriteString("  *@*) normalized_host=${normalized_host#*@} ;;\n")
	b.WriteString("esac\n")
	b.WriteString("sock=\"\"\n")
	b.WriteString("kh=\"\"\n")
	b.WriteString("case \"$normalized_host\" in\n")
	for _, key := range keys {
		if len(key.AllowedHosts) == 0 {
			continue
		}
		sock := shellQuote([]string{key.SocketPath})[0]
		kh := shellQuote([]string{key.KnownHostsPath})[0]
		patterns := strings.Join(key.AllowedHosts, "|")
		fmt.Fprintf(&b, "  %s) sock=%s; kh=%s ;;\n", patterns, sock, kh)
	}
	b.WriteString("  *) reject \"destination host not allowed: $normalized_host\" ;;\n")
	b.WriteString("esac\n")

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
	b.WriteString("  -o IdentityFile=none \\\n")
	b.WriteString("  -o StrictHostKeyChecking=yes \\\n")
	b.WriteString("  -o UserKnownHostsFile=\"$kh\" \\\n")
	b.WriteString("  -o GlobalKnownHostsFile=/dev/null \\\n")
	b.WriteString("  -o ForwardAgent=no \\\n")
	b.WriteString("  -o ClearAllForwardings=yes \\\n")
	b.WriteString("  -o IdentityAgent=\"$sock\" \\\n")
	b.WriteString("  \"$@\"\n")
	return b.String()
}
