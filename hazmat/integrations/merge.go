package integrations

import (
	"fmt"
	"os"
)

type Resolved struct {
	Spec                    Spec
	ReplaceDeclaredReadDirs bool
	AdditionalReadDirs      []string
	ResolvedEnv             map[string]string
	AdditionalWarnings      []string
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

	var result MergeResult
	result.EnvPassthrough = make(map[string]string)

	readDirSeen := make(map[string]struct{})
	excludeSeen := make(map[string]struct{})
	warnSeen := make(map[string]struct{})
	registrySeen := make(map[string]struct{})

	for _, item := range items {
		session := SessionForPlatform(item.Spec.Session, opts.Platform)
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
