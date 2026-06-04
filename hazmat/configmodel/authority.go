package configmodel

import (
	"fmt"
	"regexp"
	"strings"
)

type SandboxBackendType string
type PolicyProfileName string
type ManagedSandboxName string
type AgentName string

const (
	SandboxBackendTypeDockerSandboxes SandboxBackendType = "docker-sandboxes"
	PolicyProfileBaseline             PolicyProfileName  = "baseline"
)

var sandboxAuthorityNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type SandboxAuthority struct {
	backend *SandboxBackendAuthority
	managed []ManagedSandboxAuthority
}

func NewSandboxAuthority(config SandboxConfig) (SandboxAuthority, error) {
	var authority SandboxAuthority
	if config.Backend != nil {
		backend, err := NewSandboxBackendAuthority(*config.Backend)
		if err != nil {
			return SandboxAuthority{}, fmt.Errorf("sandbox.backend: %w", err)
		}
		authority.backend = &backend
	}
	seen := make(map[string]struct{}, len(config.Managed))
	for i, sandbox := range config.Managed {
		managed, err := NewManagedSandboxAuthority(sandbox)
		if err != nil {
			return SandboxAuthority{}, fmt.Errorf("sandbox.managed[%d]: %w", i, err)
		}
		dto := managed.DTO()
		if _, dup := seen[dto.Name]; dup {
			return SandboxAuthority{}, fmt.Errorf("sandbox.managed: duplicate sandbox name %q", dto.Name)
		}
		seen[dto.Name] = struct{}{}
		authority.managed = append(authority.managed, managed)
	}
	return authority, nil
}

func (authority SandboxAuthority) DTO() SandboxConfig {
	out := SandboxConfig{}
	if authority.backend != nil {
		backend := authority.backend.DTO()
		out.Backend = &backend
	}
	if len(authority.managed) > 0 {
		out.Managed = make([]ManagedSandboxConfig, len(authority.managed))
		for i, sandbox := range authority.managed {
			out.Managed[i] = sandbox.DTO()
		}
	}
	return out
}

func NormalizeSandboxConfig(config SandboxConfig) (SandboxConfig, error) {
	authority, err := NewSandboxAuthority(config)
	if err != nil {
		return SandboxConfig{}, err
	}
	return authority.DTO(), nil
}

type SandboxBackendAuthority struct {
	typ            SandboxBackendType
	policyProfile  PolicyProfileName
	desktopVersion string
	composeVersion string
	configuredAt   string
}

func NewSandboxBackendAuthority(input SandboxBackendConfig) (SandboxBackendAuthority, error) {
	typ, err := parseSandboxBackendType(input.Type)
	if err != nil {
		return SandboxBackendAuthority{}, err
	}
	profile, err := parsePolicyProfileName(input.PolicyProfile)
	if err != nil {
		return SandboxBackendAuthority{}, err
	}
	return SandboxBackendAuthority{
		typ:            typ,
		policyProfile:  profile,
		desktopVersion: strings.TrimSpace(input.DesktopVersion),
		composeVersion: strings.TrimSpace(input.ComposeVersion),
		configuredAt:   strings.TrimSpace(input.ConfiguredAt),
	}, nil
}

func (backend SandboxBackendAuthority) DTO() SandboxBackendConfig {
	return SandboxBackendConfig{
		Type:           string(backend.typ),
		PolicyProfile:  string(backend.policyProfile),
		DesktopVersion: backend.desktopVersion,
		ComposeVersion: backend.composeVersion,
		ConfiguredAt:   backend.configuredAt,
	}
}

type ManagedSandboxAuthority struct {
	name          ManagedSandboxName
	backendType   SandboxBackendType
	agent         AgentName
	projectDir    string
	policyProfile PolicyProfileName
	lastUsedAt    string
}

func NewManagedSandboxAuthority(input ManagedSandboxConfig) (ManagedSandboxAuthority, error) {
	name, err := parseManagedSandboxName(input.Name)
	if err != nil {
		return ManagedSandboxAuthority{}, err
	}
	backendType, err := parseSandboxBackendType(input.BackendType)
	if err != nil {
		return ManagedSandboxAuthority{}, err
	}
	agent, err := parseAgentName(input.Agent)
	if err != nil {
		return ManagedSandboxAuthority{}, err
	}
	profile, err := parsePolicyProfileName(input.PolicyProfile)
	if err != nil {
		return ManagedSandboxAuthority{}, err
	}
	projectDir := strings.TrimSpace(input.ProjectDir)
	if projectDir == "" {
		return ManagedSandboxAuthority{}, fmt.Errorf("managed sandbox %q: project is required", name)
	}
	return ManagedSandboxAuthority{
		name:          name,
		backendType:   backendType,
		agent:         agent,
		projectDir:    projectDir,
		policyProfile: profile,
		lastUsedAt:    strings.TrimSpace(input.LastUsedAt),
	}, nil
}

func (sandbox ManagedSandboxAuthority) DTO() ManagedSandboxConfig {
	return ManagedSandboxConfig{
		Name:          string(sandbox.name),
		BackendType:   string(sandbox.backendType),
		Agent:         string(sandbox.agent),
		ProjectDir:    sandbox.projectDir,
		PolicyProfile: string(sandbox.policyProfile),
		LastUsedAt:    sandbox.lastUsedAt,
	}
}

func ValidateSandboxConfig(config SandboxConfig) error {
	_, err := NewSandboxAuthority(config)
	return err
}

func parseSandboxBackendType(raw string) (SandboxBackendType, error) {
	value := SandboxBackendType(strings.TrimSpace(raw))
	switch value {
	case SandboxBackendTypeDockerSandboxes:
		return value, nil
	case "":
		return "", fmt.Errorf("backend type is required")
	default:
		return "", fmt.Errorf("unsupported sandbox backend type %q", raw)
	}
}

func parsePolicyProfileName(raw string) (PolicyProfileName, error) {
	value := PolicyProfileName(strings.TrimSpace(raw))
	switch value {
	case PolicyProfileBaseline:
		return value, nil
	case "":
		return "", fmt.Errorf("policy profile is required")
	default:
		return "", fmt.Errorf("unsupported policy profile %q", raw)
	}
}

func parseManagedSandboxName(raw string) (ManagedSandboxName, error) {
	value, err := parseAuthorityIdentifier("managed sandbox name", raw)
	return ManagedSandboxName(value), err
}

func parseAgentName(raw string) (AgentName, error) {
	value, err := parseAuthorityIdentifier("agent", raw)
	return AgentName(value), err
}

func parseAuthorityIdentifier(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !sandboxAuthorityNamePattern.MatchString(value) {
		return "", fmt.Errorf("%s %q must match %s", field, raw, sandboxAuthorityNamePattern)
	}
	return value, nil
}
