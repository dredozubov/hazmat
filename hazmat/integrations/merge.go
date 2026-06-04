package integrations

import (
	"fmt"
	"os"
	"strings"
)

type Resolved struct {
	Spec                    Spec
	ReplaceDeclaredReadDirs bool
	AdditionalReadDirs      []string
	ResolvedEnv             map[string]string
	AdditionalWarnings      []string
}

type ResolvedAuthority struct {
	resolved Resolved
	spec     AuthoritySpec
}

func NewResolvedAuthority(input Resolved) (ResolvedAuthority, error) {
	spec, err := NewAuthoritySpec(input.Spec)
	if err != nil {
		return ResolvedAuthority{}, err
	}
	resolvedEnv, err := normalizeResolvedEnv(input.ResolvedEnv)
	if err != nil {
		return ResolvedAuthority{}, err
	}
	resolved := Resolved{
		Spec:                    spec.DTO(),
		ReplaceDeclaredReadDirs: input.ReplaceDeclaredReadDirs,
		AdditionalReadDirs:      append([]string(nil), input.AdditionalReadDirs...),
		ResolvedEnv:             resolvedEnv,
		AdditionalWarnings:      append([]string(nil), input.AdditionalWarnings...),
	}
	return ResolvedAuthority{resolved: resolved, spec: spec}, nil
}

func (authority ResolvedAuthority) DTO() Resolved {
	out := authority.resolved
	out.Spec = cloneSpec(out.Spec)
	out.AdditionalReadDirs = append([]string(nil), authority.resolved.AdditionalReadDirs...)
	out.AdditionalWarnings = append([]string(nil), authority.resolved.AdditionalWarnings...)
	out.ResolvedEnv = copyStringMap(authority.resolved.ResolvedEnv)
	return out
}

func (authority ResolvedAuthority) SessionForPlatform(platform PlatformID) PlatformSession {
	return authority.spec.SessionForPlatform(platform)
}

type MergeOptions struct {
	Platform         string
	ValidateReadDirs func(Spec, string) ([]string, error)
	Getenv           func(string) string
}

type MergeResult struct {
	ReadDirs       []string
	EnvPassthrough map[string]string
	Excludes       []string
	Warnings       []string
	RegistryKeys   []string
}

func MergeResolved(items []Resolved, opts MergeOptions) (MergeResult, error) {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	platform, err := NewPlatformID(opts.Platform)
	if err != nil {
		return MergeResult{}, err
	}

	var result MergeResult
	result.EnvPassthrough = make(map[string]string)

	readDirSeen := make(map[string]struct{})
	excludeSeen := make(map[string]struct{})
	warnSeen := make(map[string]struct{})
	registrySeen := make(map[string]struct{})

	for _, raw := range items {
		authority, err := NewResolvedAuthority(raw)
		if err != nil {
			return MergeResult{}, err
		}
		item := authority.DTO()
		session := authority.SessionForPlatform(platform)
		if err := ValidateEnvKeys(item.Spec.Meta.Name, "session.env_passthrough", session.EnvPassthrough); err != nil {
			return MergeResult{}, err
		}
		if !item.ReplaceDeclaredReadDirs {
			if opts.ValidateReadDirs == nil && len(session.ReadDirs) > 0 {
				return MergeResult{}, fmt.Errorf("integration %q: read_dir validation callback is required", item.Spec.Meta.Name)
			}
			var dirs []string
			var err error
			if opts.ValidateReadDirs != nil {
				dirs, err = opts.ValidateReadDirs(item.Spec, opts.Platform)
				if err != nil {
					return MergeResult{}, err
				}
			}
			for _, dir := range dirs {
				if _, dup := readDirSeen[dir]; !dup {
					result.ReadDirs = append(result.ReadDirs, dir)
					readDirSeen[dir] = struct{}{}
				}
			}
		}

		for _, dir := range item.AdditionalReadDirs {
			if _, dup := readDirSeen[dir]; !dup {
				result.ReadDirs = append(result.ReadDirs, dir)
				readDirSeen[dir] = struct{}{}
			}
		}

		for _, key := range session.EnvPassthrough {
			if _, set := result.EnvPassthrough[key]; set {
				continue
			}
			if val := opts.Getenv(key); val != "" {
				result.EnvPassthrough[key] = val
				if registryEnvKeys[key] {
					result.RegistryKeys = append(result.RegistryKeys, key)
					registrySeen[key] = struct{}{}
				}
			}
		}
		for key, value := range item.ResolvedEnv {
			if err := RejectCredentialGrantEnvKey(fmt.Sprintf("integration %q", item.Spec.Meta.Name), "resolved env", key); err != nil {
				return MergeResult{}, err
			}
			if value == "" {
				continue
			}
			result.EnvPassthrough[key] = value
			if registryEnvKeys[key] {
				if _, dup := registrySeen[key]; !dup {
					result.RegistryKeys = append(result.RegistryKeys, key)
					registrySeen[key] = struct{}{}
				}
			}
		}

		for _, pat := range item.Spec.Backup.Excludes {
			if _, dup := excludeSeen[pat]; !dup {
				result.Excludes = append(result.Excludes, pat)
				excludeSeen[pat] = struct{}{}
			}
		}

		for _, warning := range item.Spec.Warnings {
			if _, dup := warnSeen[warning]; !dup {
				result.Warnings = append(result.Warnings, warning)
				warnSeen[warning] = struct{}{}
			}
		}
		for _, warning := range item.AdditionalWarnings {
			if _, dup := warnSeen[warning]; !dup {
				result.Warnings = append(result.Warnings, warning)
				warnSeen[warning] = struct{}{}
			}
		}
	}

	return result, nil
}

func normalizeResolvedEnv(input map[string]string) (map[string]string, error) {
	if len(input) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		normalized := strings.ToUpper(strings.TrimSpace(key))
		if normalized == "" {
			return nil, fmt.Errorf("resolved env key is required")
		}
		if _, dup := out[normalized]; dup {
			return nil, fmt.Errorf("resolved env has duplicate key after normalization: %q", normalized)
		}
		out[normalized] = value
	}
	return out, nil
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
