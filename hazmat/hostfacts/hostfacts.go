// Package hostfacts defines explicit host/platform facts passed into planners.
//
// The package is data-only: callers collect facts at frontend or runtime
// boundaries, then pass these values into pure planners.
package hostfacts

import (
	"fmt"
	"regexp"
	"strings"
)

var factIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type GOOS string
type GOARCH string
type HarnessID string
type ProbeID string
type IntegrationName string

type Platform struct {
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
}

type DockerAvailability struct {
	Available bool   `json:"available,omitempty"`
	Source    string `json:"source,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type HarnessStatus struct {
	Installed bool   `json:"installed,omitempty"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type ProbeFact struct {
	Available bool   `json:"available,omitempty"`
	Source    string `json:"source,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type IntegrationMarker struct {
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
}

type Facts struct {
	Platform           Platform                 `json:"platform"`
	AgentHome          string                   `json:"agent_home,omitempty"`
	InvokerHome        string                   `json:"invoker_home,omitempty"`
	Docker             DockerAvailability       `json:"docker,omitempty"`
	Harnesses          map[string]HarnessStatus `json:"harnesses,omitempty"`
	KernelProbes       map[string]ProbeFact     `json:"kernel_probes,omitempty"`
	PlatformProbes     map[string]ProbeFact     `json:"platform_probes,omitempty"`
	IntegrationMarkers []IntegrationMarker      `json:"integration_markers,omitempty"`
}

type HostFacts struct {
	facts Facts
}

func New(facts Facts) (HostFacts, error) {
	normalized, err := normalizeFacts(facts)
	if err != nil {
		return HostFacts{}, err
	}
	return HostFacts{facts: normalized}, nil
}

func MustNew(facts Facts) HostFacts {
	authority, err := New(facts)
	if err != nil {
		panic(err)
	}
	return authority
}

func ForPlatform(goos, goarch string) HostFacts {
	return MustNew(Facts{Platform: Platform{GOOS: goos, GOARCH: goarch}})
}

func ForGOOS(goos string) HostFacts {
	return ForPlatform(goos, "")
}

func (f Facts) TargetGOOS() string {
	return f.Platform.GOOS
}

func (f HostFacts) DTO() Facts {
	return f.facts.Normalized()
}

func (f HostFacts) Normalized() HostFacts {
	return MustNew(f.facts)
}

func (f HostFacts) TargetGOOS() string {
	return f.facts.TargetGOOS()
}

func (f HostFacts) Platform() Platform {
	return f.facts.Platform
}

func (f Facts) Normalized() Facts {
	out := f
	if len(f.Harnesses) > 0 {
		out.Harnesses = make(map[string]HarnessStatus, len(f.Harnesses))
		for key, value := range f.Harnesses {
			out.Harnesses[key] = value
		}
	}
	if len(f.KernelProbes) > 0 {
		out.KernelProbes = make(map[string]ProbeFact, len(f.KernelProbes))
		for key, value := range f.KernelProbes {
			out.KernelProbes[key] = value
		}
	}
	if len(f.PlatformProbes) > 0 {
		out.PlatformProbes = make(map[string]ProbeFact, len(f.PlatformProbes))
		for key, value := range f.PlatformProbes {
			out.PlatformProbes[key] = value
		}
	}
	if len(f.IntegrationMarkers) > 0 {
		out.IntegrationMarkers = append([]IntegrationMarker(nil), f.IntegrationMarkers...)
	}
	return out
}

func normalizeFacts(input Facts) (Facts, error) {
	out := input.Normalized()
	var err error
	out.Platform.GOOS, err = normalizeOptionalIdentifier("platform.goos", out.Platform.GOOS)
	if err != nil {
		return Facts{}, err
	}
	out.Platform.GOARCH, err = normalizeOptionalIdentifier("platform.goarch", out.Platform.GOARCH)
	if err != nil {
		return Facts{}, err
	}
	if out.Harnesses, err = normalizeStatusMap("harnesses", out.Harnesses); err != nil {
		return Facts{}, err
	}
	if out.KernelProbes, err = normalizeProbeMap("kernel_probes", out.KernelProbes); err != nil {
		return Facts{}, err
	}
	if out.PlatformProbes, err = normalizeProbeMap("platform_probes", out.PlatformProbes); err != nil {
		return Facts{}, err
	}
	for i := range out.IntegrationMarkers {
		name, err := normalizeRequiredIdentifier(fmt.Sprintf("integration_markers[%d].name", i), out.IntegrationMarkers[i].Name)
		if err != nil {
			return Facts{}, err
		}
		out.IntegrationMarkers[i].Name = name
		if strings.ContainsRune(out.IntegrationMarkers[i].Source, '\x00') {
			return Facts{}, fmt.Errorf("integration_markers[%d].source contains NUL", i)
		}
	}
	return out, nil
}

func normalizeStatusMap(field string, input map[string]HarnessStatus) (map[string]HarnessStatus, error) {
	if len(input) == 0 {
		return map[string]HarnessStatus{}, nil
	}
	out := make(map[string]HarnessStatus, len(input))
	for raw, value := range input {
		key, err := normalizeRequiredIdentifier(field+" key", raw)
		if err != nil {
			return nil, err
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("%s has duplicate key after normalization: %q", field, key)
		}
		out[key] = value
	}
	return out, nil
}

func normalizeProbeMap(field string, input map[string]ProbeFact) (map[string]ProbeFact, error) {
	if len(input) == 0 {
		return map[string]ProbeFact{}, nil
	}
	out := make(map[string]ProbeFact, len(input))
	for raw, value := range input {
		key, err := normalizeRequiredIdentifier(field+" key", raw)
		if err != nil {
			return nil, err
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("%s has duplicate key after normalization: %q", field, key)
		}
		out[key] = value
	}
	return out, nil
}

func normalizeOptionalIdentifier(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !factIdentifierPattern.MatchString(value) {
		return "", fmt.Errorf("%s %q must match %s", field, raw, factIdentifierPattern)
	}
	return value, nil
}

func normalizeRequiredIdentifier(field, raw string) (string, error) {
	value, err := normalizeOptionalIdentifier(field, raw)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return value, nil
}
