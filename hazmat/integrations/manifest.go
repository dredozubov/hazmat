package integrations

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Spec struct {
	Meta     Meta              `yaml:"integration"`
	Detect   Detect            `yaml:"detect"`
	Session  Session           `yaml:"session"`
	Backup   Backup            `yaml:"backup"`
	Warnings []string          `yaml:"warnings"`
	Commands map[string]string `yaml:"commands"`
}

type Meta struct {
	Name        string `yaml:"name"`
	Version     int    `yaml:"version"`
	Description string `yaml:"description"`
}

type Detect struct {
	Files    []string `yaml:"files"`
	RootDirs []string `yaml:"root_dirs"`
}

type Session struct {
	ReadDirs       []string                   `yaml:"read_dirs"`
	EnvPassthrough []string                   `yaml:"env_passthrough"`
	Platforms      map[string]PlatformSession `yaml:"platforms"`
}

type PlatformSession struct {
	ReadDirs       []string `yaml:"read_dirs"`
	EnvPassthrough []string `yaml:"env_passthrough"`
}

type Backup struct {
	Excludes []string `yaml:"excludes"`
}

const (
	MaxSize           = 8192
	MaxReadDirs       = 20
	MaxEnvKeys        = 20
	MaxExcludes       = 50
	MaxWarnings       = 10
	MaxCommands       = 20
	MaxDetectFiles    = 10
	MaxDetectRootDirs = 10
)

var NamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var RootDirNamePattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

var manifestPlatforms = map[string]struct{}{
	"darwin": {},
	"linux":  {},
}

var safeEnvKeys = map[string]bool{
	"GOPATH":       true,
	"GOROOT":       true,
	"GOPROXY":      true,
	"GONOPROXY":    true,
	"GONOSUMCHECK": true,
	"GOPRIVATE":    true,
	"CGO_ENABLED":  true,

	"RUSTUP_HOME":      true,
	"CARGO_HOME":       true,
	"CARGO_TARGET_DIR": true,

	"NODE_ENV":            true,
	"NPM_CONFIG_REGISTRY": true,
	"PNPM_HOME":           true,
	"BUN_INSTALL":         true,
	"YARN_CACHE_FOLDER":   true,
	"DENO_DIR":            true,
	"DENO_INSTALL":        true,

	"VIRTUAL_ENV": true,

	"JAVA_HOME":     true,
	"TLA2TOOLS_JAR": true,

	"DEVELOPER_DIR": true,

	"DOTNET_ROOT":    true,
	"NUGET_PACKAGES": true,

	"ANDROID_HOME":     true,
	"ANDROID_SDK_ROOT": true,
	"ANDROID_NDK_HOME": true,
	"GRADLE_USER_HOME": true,

	"FLUTTER_ROOT": true,
	"PUB_CACHE":    true,

	"CMAKE_PREFIX_PATH": true,
	"CMAKE_GENERATOR":   true,

	"GEM_HOME": true,

	"COMPOSER_HOME":      true,
	"COMPOSER_CACHE_DIR": true,

	"DOCKER_HOST":       true,
	"DOCKER_TLS_VERIFY": true,

	"EDITOR": true,
	"VISUAL": true,
}

var registryEnvKeys = map[string]bool{
	"GOPROXY":             true,
	"NPM_CONFIG_REGISTRY": true,
}

func SafeEnvKeys() map[string]bool {
	return copyBoolMap(safeEnvKeys)
}

func RegistryEnvKeys() map[string]bool {
	return copyBoolMap(registryEnvKeys)
}

func ManifestPlatforms() map[string]struct{} {
	out := make(map[string]struct{}, len(manifestPlatforms))
	for key := range manifestPlatforms {
		out[key] = struct{}{}
	}
	return out
}

func IsSafeEnvKey(key string) bool {
	return safeEnvKeys[key]
}

func IsRegistryEnvKey(key string) bool {
	return registryEnvKeys[key]
}

func IsCredentialGrantEnvKey(key string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(key))
	switch normalized {
	case "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"GH_TOKEN", "GITHUB_TOKEN", "GITHUB_PAT", "SSH_AUTH_SOCK":
		return true
	}
	if strings.HasSuffix(normalized, "_API_KEY") ||
		strings.HasSuffix(normalized, "_PRIVATE_KEY") ||
		strings.HasSuffix(normalized, "_ACCESS_KEY") ||
		strings.HasSuffix(normalized, "_ACCESS_TOKEN") {
		return true
	}
	for _, part := range strings.Split(normalized, "_") {
		switch part {
		case "TOKEN", "SECRET", "PASSWORD":
			return true
		}
	}
	return false
}

func RejectCredentialGrantEnvKey(owner, field, key string) error {
	if !IsCredentialGrantEnvKey(key) {
		return nil
	}
	return fmt.Errorf("%s: env key %q in %s is credential/capability-shaped; use a SecretRef or capability grant instead of env passthrough", owner, key, field)
}

func ValidateEnvKeys(integrationName, field string, keys []string) error {
	for _, key := range keys {
		if err := RejectCredentialGrantEnvKey(fmt.Sprintf("integration %q", integrationName), field, key); err != nil {
			return err
		}
		if !safeEnvKeys[key] {
			return fmt.Errorf("integration %q: env key %q in %s not in safe passthrough set", integrationName, key, field)
		}
	}
	return nil
}

func ValidateSchema(p Spec) error {
	if p.Meta.Name == "" {
		return fmt.Errorf("integration: name is required")
	}
	if !NamePattern.MatchString(p.Meta.Name) {
		return fmt.Errorf("integration %q: name must match %s", p.Meta.Name, NamePattern)
	}
	if p.Meta.Version != 1 {
		return fmt.Errorf("integration %q: version must be 1, got %d", p.Meta.Name, p.Meta.Version)
	}

	if err := ValidateEnvKeys(p.Meta.Name, "session.env_passthrough", p.Session.EnvPassthrough); err != nil {
		return err
	}
	for platform, session := range p.Session.Platforms {
		if _, ok := manifestPlatforms[platform]; !ok {
			return fmt.Errorf("integration %q: unsupported session platform %q", p.Meta.Name, platform)
		}
		if err := ValidateEnvKeys(p.Meta.Name, "session.platforms."+platform+".env_passthrough", session.EnvPassthrough); err != nil {
			return err
		}
	}

	for _, pat := range p.Backup.Excludes {
		if pat == "" {
			return fmt.Errorf("integration %q: empty exclude pattern", p.Meta.Name)
		}
		if strings.HasPrefix(pat, "!") {
			return fmt.Errorf("integration %q: negation excludes not supported: %q", p.Meta.Name, pat)
		}
	}

	for _, f := range p.Detect.Files {
		if strings.Contains(f, "/") {
			return fmt.Errorf("integration %q: detect file %q must be a filename, not a path", p.Meta.Name, f)
		}
	}

	for _, d := range p.Detect.RootDirs {
		if d == "" {
			return fmt.Errorf("integration %q: empty detect root_dir", p.Meta.Name)
		}
		if d == "." || d == ".." {
			return fmt.Errorf("integration %q: detect root_dir %q is not a valid project marker", p.Meta.Name, d)
		}
		if strings.ContainsAny(d, "/\\\x00") {
			return fmt.Errorf("integration %q: detect root_dir %q must be a single path component", p.Meta.Name, d)
		}
		if strings.ContainsFunc(d, func(r rune) bool {
			switch r {
			case ' ', '\t', '\n', '\r':
				return true
			}
			return false
		}) {
			return fmt.Errorf("integration %q: detect root_dir %q must not contain whitespace", p.Meta.Name, d)
		}
		if !RootDirNamePattern.MatchString(d) {
			return fmt.Errorf("integration %q: detect root_dir %q must match %s", p.Meta.Name, d, RootDirNamePattern)
		}
	}

	if len(p.Session.ReadDirs) > MaxReadDirs {
		return fmt.Errorf("integration %q: too many read_dirs (%d, max %d)", p.Meta.Name, len(p.Session.ReadDirs), MaxReadDirs)
	}
	if len(p.Session.EnvPassthrough) > MaxEnvKeys {
		return fmt.Errorf("integration %q: too many env_passthrough keys (%d, max %d)", p.Meta.Name, len(p.Session.EnvPassthrough), MaxEnvKeys)
	}
	for platform := range p.Session.Platforms {
		effective := SessionForPlatform(p.Session, platform)
		if len(effective.ReadDirs) > MaxReadDirs {
			return fmt.Errorf("integration %q: too many %s read_dirs (%d, max %d)", p.Meta.Name, platform, len(effective.ReadDirs), MaxReadDirs)
		}
		if len(effective.EnvPassthrough) > MaxEnvKeys {
			return fmt.Errorf("integration %q: too many %s env_passthrough keys (%d, max %d)", p.Meta.Name, platform, len(effective.EnvPassthrough), MaxEnvKeys)
		}
	}
	if len(p.Backup.Excludes) > MaxExcludes {
		return fmt.Errorf("integration %q: too many excludes (%d, max %d)", p.Meta.Name, len(p.Backup.Excludes), MaxExcludes)
	}
	if len(p.Warnings) > MaxWarnings {
		return fmt.Errorf("integration %q: too many warnings (%d, max %d)", p.Meta.Name, len(p.Warnings), MaxWarnings)
	}
	if len(p.Commands) > MaxCommands {
		return fmt.Errorf("integration %q: too many commands (%d, max %d)", p.Meta.Name, len(p.Commands), MaxCommands)
	}
	if len(p.Detect.Files) > MaxDetectFiles {
		return fmt.Errorf("integration %q: too many detect files (%d, max %d)", p.Meta.Name, len(p.Detect.Files), MaxDetectFiles)
	}
	if len(p.Detect.RootDirs) > MaxDetectRootDirs {
		return fmt.Errorf("integration %q: too many detect root_dirs (%d, max %d)", p.Meta.Name, len(p.Detect.RootDirs), MaxDetectRootDirs)
	}

	return nil
}

func SessionForPlatform(session Session, platform string) PlatformSession {
	effective := PlatformSession{
		ReadDirs:       append([]string(nil), session.ReadDirs...),
		EnvPassthrough: append([]string(nil), session.EnvPassthrough...),
	}
	if session.Platforms == nil {
		return effective
	}
	overlay, ok := session.Platforms[platform]
	if !ok {
		return effective
	}
	effective.ReadDirs = append(effective.ReadDirs, overlay.ReadDirs...)
	effective.EnvPassthrough = append(effective.EnvPassthrough, overlay.EnvPassthrough...)
	return effective
}

func LoadSpec(data []byte) (Spec, error) {
	if len(data) > MaxSize {
		return Spec{}, fmt.Errorf("integration manifest exceeds %d byte limit", MaxSize)
	}
	if HasLegacyTopLevelKey(data, "pack") {
		return Spec{}, fmt.Errorf("legacy integration manifest schema detected: rename top-level key 'pack:' to 'integration:'")
	}

	var spec Spec
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&spec); err != nil {
		return Spec{}, fmt.Errorf("parse integration manifest: %w", err)
	}
	if err := ValidateSchema(spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func HasLegacyTopLevelKey(data []byte, key string) bool {
	trimmed := bytes.TrimSpace(data)
	prefix := []byte(key + ":")
	return bytes.HasPrefix(trimmed, prefix) || bytes.Contains(data, append([]byte("\n"), prefix...))
}

func copyBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
