package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type resolvedIntegration struct {
	Spec                    IntegrationSpec
	ReplaceDeclaredReadDirs bool
	AdditionalReadDirs      []string
	ResolvedEnv             map[string]string
	AdditionalWarnings      []string
	Source                  string
	Details                 []string
}

type integrationResolverSpec struct {
	Summary                  string
	ReplacesDeclaredReadDirs bool
	Resolve                  func(*integrationResolveContext, IntegrationSpec) (resolvedIntegration, error)
}

type integrationProbe interface {
	LookPath(name string) (string, error)
	Output(name string, args ...string) (string, error)
}

type hostIntegrationProbe struct{}

type integrationResolveContext struct {
	ProjectDir string
	Probe      integrationProbe
	homebrew   *integrationHomebrewResolver
}

type integrationHomebrewResolver struct {
	projectDir string
	probe      integrationProbe
}

type brewPrefixResult struct {
	Prefix  string
	Formula string
	Detail  string
}

var (
	integrationProbeFactory    = func() integrationProbe { return hostIntegrationProbe{} }
	integrationProbeTimeout    = 2 * time.Second
	integrationHomebrewTimeout = 10 * time.Second
	integrationBrewCandidates  = []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"}
	integrationJavaHomePath    = "/usr/libexec/java_home"
	integrationAgentExecCheck  = func(path string) bool { return pathExecutableByAgent(path) }
	homebrewConsentPrompt      = func() (bool, bool) {
		if flagDryRun {
			return false, false
		}
		ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
		if !ui.IsInteractive() && !flagYesAll {
			return false, false
		}
		allowed := ui.Ask("Allow Homebrew-backed path resolution for session integrations? This does not itself grant new filesystem access; Hazmat will still show the resolved directories before launch.")
		return allowed, true
	}
)

var builtinIntegrationResolvers = map[string]integrationResolverSpec{
	"go": {
		Summary: "runtime go env probe with Homebrew go fallback",
		Resolve: resolveGoIntegration,
	},
	"haskell-cabal": {
		Summary: "ghc/cabal runtime probe with Homebrew ghc and cabal-install fallback",
		Resolve: resolveHaskellCabalIntegration,
	},
	"java-gradle": {
		Summary: "JDK runtime probe with Homebrew openjdk and gradle fallback",
		Resolve: func(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
			return resolveJavaBuildIntegration(ctx, spec, "java-gradle", "gradle", "gradle")
		},
	},
	"java-maven": {
		Summary: "JDK runtime probe with Homebrew openjdk and maven fallback",
		Resolve: func(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
			return resolveJavaBuildIntegration(ctx, spec, "java-maven", "mvn", "maven")
		},
	},
	"node": {
		Summary:                  "active Node runtime probe with Homebrew node fallback",
		ReplacesDeclaredReadDirs: true,
		Resolve:                  resolveNodeIntegration,
	},
	"opentofu-plan": {
		Summary: "tofu runtime probe with Homebrew opentofu fallback",
		Resolve: resolveOpenTofuIntegration,
	},
	"ruby-bundler": {
		Summary: "ruby runtime probe with Homebrew ruby fallback",
		Resolve: resolveRubyBundlerIntegration,
	},
	"rust": {
		Summary:                  "rustc sysroot probe with Homebrew rust/rustup fallback",
		ReplacesDeclaredReadDirs: true,
		Resolve:                  resolveRustIntegration,
	},
	"elixir-mix": {
		Summary: "elixir/erl runtime probe with Homebrew elixir and erlang fallback",
		Resolve: resolveElixirMixIntegration,
	},
	"tla-java": {
		Summary:                  "JAVA_HOME / java runtime probe with Homebrew openjdk fallback",
		ReplacesDeclaredReadDirs: true,
		Resolve:                  resolveTLAJavaIntegration,
	},
}

func (hostIntegrationProbe) LookPath(name string) (string, error) {
	return commandPathFromEnv(name, integrationProbeEnv())
}

func (hostIntegrationProbe) Output(name string, args ...string) (string, error) {
	timeout := integrationTimeoutForCommand(name)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	env := integrationProbeEnv()
	resolvedName, err := commandPathFromEnv(name, env)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, resolvedName, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%s timed out after %s", commandLabel(name, args...), timeout)
	}
	return strings.TrimSpace(string(out)), err
}

func integrationProbeEnv() []string {
	env := []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + defaultAgentPath,
		"HOMEBREW_NO_AUTO_UPDATE=1",
	}
	// Include safe path pointers from the invoker's environment so probes
	// like "go env GOMODCACHE" resolve correctly when the invoker has a
	// non-default GOPATH, CARGO_HOME, etc.
	for _, key := range []string{"LANG", "LC_ALL", "LC_CTYPE", "TERM",
		"GOPATH", "GOMODCACHE", "RUSTUP_HOME", "CARGO_HOME", "JAVA_HOME"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func commandPathFromEnv(name string, env []string) (string, error) {
	if name == "" {
		return "", exec.ErrNotFound
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		return name, nil
	}

	pathValue := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			pathValue = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	if pathValue == "" {
		pathValue = os.Getenv("PATH")
	}

	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("%w: %s", exec.ErrNotFound, name)
}

func commandLabel(name string, args ...string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}

func integrationTimeoutForCommand(name string) time.Duration {
	if filepath.Base(name) == "brew" {
		return integrationHomebrewTimeout
	}
	return integrationProbeTimeout
}

func resolveRuntimeIntegrations(projectDir string, integrations []IntegrationSpec) ([]resolvedIntegration, error) {
	ctx := &integrationResolveContext{
		ProjectDir: projectDir,
		Probe:      integrationProbeFactory(),
	}

	resolved := make([]resolvedIntegration, 0, len(integrations))
	for _, integration := range integrations {
		r := resolvedIntegration{Spec: integration}
		if spec, ok := builtinIntegrationResolvers[integration.Meta.Name]; ok {
			var err error
			r, err = spec.Resolve(ctx, integration)
			if err != nil {
				return nil, err
			}
			r.Spec = integration
			r.ReplaceDeclaredReadDirs = spec.ReplacesDeclaredReadDirs
		}
		resolved = append(resolved, r)
	}
	return resolved, nil
}

func integrationResolverFor(name string) (integrationResolverSpec, bool) {
	spec, ok := builtinIntegrationResolvers[name]
	return spec, ok
}

func resolveGoIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec, ResolvedEnv: make(map[string]string)}
	if dir, err := probeCanonicalDir(ctx.Probe, "go", "env", "GOROOT"); err == nil && dir != "" {
		if runtimeDir, err := validatedRuntimeDir(dir, filepath.Join("bin", "go")); err == nil && runtimeDir != "" {
			result.AdditionalReadDirs = []string{runtimeDir}
			result.Source = "go (go env GOROOT)"
			result.Details = append(result.Details, fmt.Sprintf("go: resolved GOROOT via go env -> %s", runtimeDir))
			if os.Getenv("GOROOT") == "" {
				result.ResolvedEnv["GOROOT"] = runtimeDir
			}
			repairGoCompanionTools(ctx, &result)
			return result, nil
		}
		result.Details = append(result.Details, fmt.Sprintf("go: resolved GOROOT via go env -> %s, but %s cannot execute %s", dir, agentUser, filepath.Join(dir, "bin", "go")))
	}

	brewResult := ctx.brewPrefix("go")
	if brewResult.Prefix != "" {
		if dir := goRootFromPrefix(brewResult.Prefix); dir != "" {
			result.AdditionalReadDirs = []string{dir}
			result.Source = fmt.Sprintf("go (Homebrew %s)", brewResult.Formula)
			result.Details = append(result.Details, fmt.Sprintf("go: resolved via Homebrew %s -> %s", brewResult.Formula, dir))
			if os.Getenv("GOROOT") == "" {
				result.ResolvedEnv["GOROOT"] = dir
			}
			repairGoCompanionTools(ctx, &result)
		} else {
			result.Details = append(result.Details, fmt.Sprintf("go: Homebrew %s is installed, but %s cannot execute %s", brewResult.Formula, agentUser, filepath.Join(brewResult.Prefix, "libexec", "bin", "go")))
		}
	} else if brewResult.Detail != "" {
		result.Details = append(result.Details, "go: "+brewResult.Detail)
	}
	return result, nil
}

// repairGoCompanionTools fixes Homebrew permissions for common Go development
// tools that are separate formulae (e.g., golangci-lint). Called after Go
// itself is successfully resolved.
func repairGoCompanionTools(ctx *integrationResolveContext, result *resolvedIntegration) {
	for _, tool := range []string{"golangci-lint"} {
		brewResult := ctx.brewPrefix(tool)
		if brewResult.Prefix == "" {
			continue
		}
		binPath := filepath.Join(brewResult.Prefix, "bin", tool)
		if _, err := os.Stat(binPath); err != nil {
			continue
		}
		if integrationAgentExecCheck(binPath) {
			continue
		}
		if repairHomebrewToolAccess(brewResult.Prefix) {
			result.Details = append(result.Details, fmt.Sprintf("go: repaired %s Homebrew access at %s", tool, brewResult.Prefix))
		}
	}
}

func resolveNodeIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec}
	if execPath, err := ctx.Probe.Output("node", "-p", "process.execPath"); err == nil && execPath != "" {
		prefix := filepath.Dir(filepath.Dir(strings.TrimSpace(execPath)))
		dir, err := validatedRuntimeDir(prefix, filepath.Join("bin", "node"))
		if err == nil && dir != "" {
			result.AdditionalReadDirs = []string{dir}
			result.Source = "node (active runtime)"
			result.Details = append(result.Details, fmt.Sprintf("node: resolved active runtime prefix -> %s", dir))
			return result, nil
		}
	}

	brewResult := ctx.brewPrefix("node")
	if brewResult.Prefix != "" {
		dir, err := validatedRuntimeDir(brewResult.Prefix, filepath.Join("bin", "node"))
		if err == nil && dir != "" {
			result.AdditionalReadDirs = []string{dir}
			result.Source = fmt.Sprintf("node (Homebrew %s)", brewResult.Formula)
			result.Details = append(result.Details, fmt.Sprintf("node: resolved via Homebrew %s -> %s", brewResult.Formula, dir))
		}
	} else if brewResult.Detail != "" {
		result.Details = append(result.Details, "node: "+brewResult.Detail)
	}
	return result, nil
}

func resolveHaskellCabalIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec}
	var sourceParts []string
	addDir := func(dir, sourcePart, detail string) {
		for _, existing := range result.AdditionalReadDirs {
			if existing == dir {
				return
			}
		}
		result.AdditionalReadDirs = append(result.AdditionalReadDirs, dir)
		sourceParts = append(sourceParts, sourcePart)
		result.Details = append(result.Details, detail)
	}

	if dir, err := probeCommandPrefix(ctx.Probe, "ghc"); err == nil && dir != "" {
		addDir(dir, "ghc runtime", fmt.Sprintf("haskell-cabal: resolved ghc runtime prefix -> %s", dir))
	} else {
		brewResult := ctx.brewPrefix("ghc")
		if brewResult.Prefix != "" {
			addDir(brewResult.Prefix, "Homebrew "+brewResult.Formula, fmt.Sprintf("haskell-cabal: resolved ghc via Homebrew %s -> %s", brewResult.Formula, brewResult.Prefix))
		} else if brewResult.Detail != "" {
			result.Details = append(result.Details, "haskell-cabal: ghc "+brewResult.Detail)
		}
	}

	if dir, err := probeCommandPrefix(ctx.Probe, "cabal"); err == nil && dir != "" {
		addDir(dir, "cabal runtime", fmt.Sprintf("haskell-cabal: resolved cabal runtime prefix -> %s", dir))
	} else {
		brewResult := ctx.brewPrefix("cabal-install")
		if brewResult.Prefix != "" {
			addDir(brewResult.Prefix, "Homebrew "+brewResult.Formula, fmt.Sprintf("haskell-cabal: resolved cabal via Homebrew %s -> %s", brewResult.Formula, brewResult.Prefix))
		} else if brewResult.Detail != "" {
			result.Details = append(result.Details, "haskell-cabal: cabal "+brewResult.Detail)
		}
	}

	result.Source = integrationSource("haskell-cabal", sourceParts)
	return result, nil
}

func resolveRustIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec}
	if dir, err := probeCanonicalDir(ctx.Probe, "rustc", "--print", "sysroot"); err == nil && dir != "" {
		if runtimeDir, err := validatedRuntimeDir(dir, filepath.Join("bin", "rustc")); err == nil && runtimeDir != "" {
			result.AdditionalReadDirs = []string{runtimeDir}
			result.Source = "rust (rustc sysroot)"
			result.Details = append(result.Details, fmt.Sprintf("rust: resolved sysroot via rustc -> %s", runtimeDir))
			return result, nil
		}
		result.Details = append(result.Details, fmt.Sprintf("rust: resolved sysroot via rustc -> %s, but %s cannot execute %s", dir, agentUser, filepath.Join(dir, "bin", "rustc")))
	}

	brewResult := ctx.brewPrefix("rust", "rustup")
	if brewResult.Prefix != "" {
		dir, err := validatedRuntimeDir(brewResult.Prefix, filepath.Join("bin", "rustc"))
		if err == nil && dir != "" {
			result.AdditionalReadDirs = []string{dir}
			result.Source = fmt.Sprintf("rust (Homebrew %s)", brewResult.Formula)
			result.Details = append(result.Details, fmt.Sprintf("rust: resolved via Homebrew %s -> %s", brewResult.Formula, dir))
		}
	} else if brewResult.Detail != "" {
		result.Details = append(result.Details, "rust: "+brewResult.Detail)
	}
	return result, nil
}

func resolveJavaBuildIntegration(ctx *integrationResolveContext, spec IntegrationSpec, integrationName, toolCommand string, brewFormulas ...string) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec, ResolvedEnv: make(map[string]string)}
	var sourceParts []string
	addDir := func(dir, sourcePart, detail string) {
		for _, existing := range result.AdditionalReadDirs {
			if existing == dir {
				return
			}
		}
		result.AdditionalReadDirs = append(result.AdditionalReadDirs, dir)
		sourceParts = append(sourceParts, sourcePart)
		result.Details = append(result.Details, detail)
	}

	if javaHome, source, err := ctx.resolveJavaHome(); err == nil && javaHome != "" {
		addDir(javaHome, javaIntegrationSourcePart(source), fmt.Sprintf("%s: resolved JDK home -> %s", integrationName, javaHome))
		if shouldSetResolvedJavaHomeEnv() {
			result.ResolvedEnv["JAVA_HOME"] = javaHome
		}
	} else {
		brewResult := ctx.brewPrefix("openjdk", "openjdk@21", "openjdk@17")
		if brewResult.Prefix != "" {
			if javaHome := javaHomeFromPrefix(brewResult.Prefix); javaHome != "" {
				addDir(javaHome, "Homebrew "+brewResult.Formula, fmt.Sprintf("%s: resolved JDK via Homebrew %s -> %s", integrationName, brewResult.Formula, javaHome))
				if shouldSetResolvedJavaHomeEnv() {
					result.ResolvedEnv["JAVA_HOME"] = javaHome
				}
			}
		} else if brewResult.Detail != "" {
			result.Details = append(result.Details, integrationName+": JDK "+brewResult.Detail)
		}
	}

	if dir, err := probeCommandPrefix(ctx.Probe, toolCommand); err == nil && dir != "" {
		addDir(dir, toolCommand+" runtime", fmt.Sprintf("%s: resolved %s runtime prefix -> %s", integrationName, toolCommand, dir))
	} else {
		brewResult := ctx.brewPrefix(brewFormulas...)
		if brewResult.Prefix != "" {
			addDir(brewResult.Prefix, "Homebrew "+brewResult.Formula, fmt.Sprintf("%s: resolved %s via Homebrew %s -> %s", integrationName, toolCommand, brewResult.Formula, brewResult.Prefix))
		} else if brewResult.Detail != "" {
			result.Details = append(result.Details, integrationName+": "+toolCommand+" "+brewResult.Detail)
		}
	}

	result.Source = integrationSource(integrationName, sourceParts)
	return result, nil
}

func resolveTLAJavaIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec, ResolvedEnv: make(map[string]string)}
	if javaHome, source, err := ctx.resolveJavaHome(); err == nil && javaHome != "" {
		result.AdditionalReadDirs = []string{javaHome}
		result.Source = source
		result.Details = append(result.Details, fmt.Sprintf("tla-java: resolved JDK home -> %s", javaHome))
		if shouldSetResolvedJavaHomeEnv() {
			result.ResolvedEnv["JAVA_HOME"] = javaHome
		}
		return result, nil
	}

	brewResult := ctx.brewPrefix("openjdk", "openjdk@21", "openjdk@17")
	if brewResult.Prefix != "" {
		if javaHome := javaHomeFromPrefix(brewResult.Prefix); javaHome != "" {
			result.AdditionalReadDirs = []string{javaHome}
			result.Source = fmt.Sprintf("tla-java (Homebrew %s)", brewResult.Formula)
			result.Details = append(result.Details, fmt.Sprintf("tla-java: resolved via Homebrew %s -> %s", brewResult.Formula, javaHome))
			if shouldSetResolvedJavaHomeEnv() {
				result.ResolvedEnv["JAVA_HOME"] = javaHome
			}
		}
	} else if brewResult.Detail != "" {
		result.Details = append(result.Details, "tla-java: "+brewResult.Detail)
	}
	return result, nil
}

func resolveRubyBundlerIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec}
	if dir, err := probeCommandPrefix(ctx.Probe, "ruby"); err == nil && dir != "" {
		result.AdditionalReadDirs = []string{dir}
		result.Source = "ruby-bundler (ruby runtime)"
		result.Details = append(result.Details, fmt.Sprintf("ruby-bundler: resolved ruby runtime prefix -> %s", dir))
		return result, nil
	}

	brewResult := ctx.brewPrefix("ruby")
	if brewResult.Prefix != "" {
		result.AdditionalReadDirs = []string{brewResult.Prefix}
		result.Source = fmt.Sprintf("ruby-bundler (Homebrew %s)", brewResult.Formula)
		result.Details = append(result.Details, fmt.Sprintf("ruby-bundler: resolved via Homebrew %s -> %s", brewResult.Formula, brewResult.Prefix))
	} else if brewResult.Detail != "" {
		result.Details = append(result.Details, "ruby-bundler: "+brewResult.Detail)
	}
	return result, nil
}

func resolveElixirMixIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec}
	var sourceParts []string
	addDir := func(dir, sourcePart, detail string) {
		for _, existing := range result.AdditionalReadDirs {
			if existing == dir {
				return
			}
		}
		result.AdditionalReadDirs = append(result.AdditionalReadDirs, dir)
		sourceParts = append(sourceParts, sourcePart)
		result.Details = append(result.Details, detail)
	}

	if dir, err := probeCommandPrefix(ctx.Probe, "elixir"); err == nil && dir != "" {
		addDir(dir, "elixir runtime", fmt.Sprintf("elixir-mix: resolved elixir runtime prefix -> %s", dir))
	} else {
		brewResult := ctx.brewPrefix("elixir")
		if brewResult.Prefix != "" {
			addDir(brewResult.Prefix, "Homebrew "+brewResult.Formula, fmt.Sprintf("elixir-mix: resolved elixir via Homebrew %s -> %s", brewResult.Formula, brewResult.Prefix))
		} else if brewResult.Detail != "" {
			result.Details = append(result.Details, "elixir-mix: elixir "+brewResult.Detail)
		}
	}

	if dir, err := probeCommandPrefix(ctx.Probe, "erl"); err == nil && dir != "" {
		addDir(dir, "erlang runtime", fmt.Sprintf("elixir-mix: resolved erlang runtime prefix -> %s", dir))
	} else {
		brewResult := ctx.brewPrefix("erlang")
		if brewResult.Prefix != "" {
			addDir(brewResult.Prefix, "Homebrew "+brewResult.Formula, fmt.Sprintf("elixir-mix: resolved erlang via Homebrew %s -> %s", brewResult.Formula, brewResult.Prefix))
		} else if brewResult.Detail != "" {
			result.Details = append(result.Details, "elixir-mix: erlang "+brewResult.Detail)
		}
	}

	result.Source = integrationSource("elixir-mix", sourceParts)
	return result, nil
}

func resolveOpenTofuIntegration(ctx *integrationResolveContext, spec IntegrationSpec) (resolvedIntegration, error) {
	result := resolvedIntegration{Spec: spec}
	if dir, err := probeCommandPrefix(ctx.Probe, "tofu"); err == nil && dir != "" {
		result.AdditionalReadDirs = []string{dir}
		result.Source = "opentofu-plan (tofu runtime)"
		result.Details = append(result.Details, fmt.Sprintf("opentofu-plan: resolved tofu runtime prefix -> %s", dir))
		return result, nil
	}

	brewResult := ctx.brewPrefix("opentofu")
	if brewResult.Prefix != "" {
		result.AdditionalReadDirs = []string{brewResult.Prefix}
		result.Source = fmt.Sprintf("opentofu-plan (Homebrew %s)", brewResult.Formula)
		result.Details = append(result.Details, fmt.Sprintf("opentofu-plan: resolved via Homebrew %s -> %s", brewResult.Formula, brewResult.Prefix))
	} else if brewResult.Detail != "" {
		result.Details = append(result.Details, "opentofu-plan: "+brewResult.Detail)
	}
	return result, nil
}

func probeCanonicalDir(probe integrationProbe, name string, args ...string) (string, error) {
	output, err := probe.Output(name, args...)
	if err != nil {
		return "", err
	}
	return validatedReadDir(output)
}

func validatedReadDir(path string) (string, error) {
	path = strings.TrimSpace(expandTilde(path))
	if path == "" {
		return "", nil
	}

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", err
	}

	resolved, err := canonicalizePath(path)
	if err != nil {
		return "", err
	}
	if isCredentialDenyPath(resolved) {
		return "", fmt.Errorf("%q resolves to credential deny zone", resolved)
	}
	return resolved, nil
}

func probeCommandPrefix(probe integrationProbe, name string) (string, error) {
	execPath, err := probe.LookPath(name)
	if err != nil {
		return "", err
	}
	execPath = strings.TrimSpace(execPath)
	if execPath == "" {
		return "", nil
	}
	if resolvedPath, err := filepath.EvalSymlinks(execPath); err == nil && resolvedPath != "" {
		execPath = resolvedPath
	}
	prefix := filepath.Dir(filepath.Dir(execPath))
	return validatedToolchainPrefix(prefix, filepath.Join("bin", filepath.Base(execPath)))
}

func validatedToolchainPrefix(path, executableRel string) (string, error) {
	dir, err := validatedRuntimeDir(path, executableRel)
	if err != nil || dir == "" {
		return "", err
	}
	if genericToolchainRoot(dir) {
		return "", fmt.Errorf("%q is a generic system prefix, not a bounded toolchain root", dir)
	}
	return dir, nil
}

func genericToolchainRoot(dir string) bool {
	switch filepath.Clean(dir) {
	case "/", "/System", "/Library", "/bin", "/sbin", "/usr", "/usr/local", "/opt", "/opt/homebrew":
		return true
	default:
		return false
	}
}

func integrationSource(name string, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("%s (%s)", name, strings.Join(parts, ", "))
}

func javaIntegrationSourcePart(source string) string {
	switch {
	case strings.Contains(source, "JAVA_HOME"):
		return "JAVA_HOME"
	case strings.Contains(source, "java_home"):
		return "java_home"
	case strings.Contains(source, "java runtime"):
		return "java runtime"
	default:
		return "JDK runtime"
	}
}

func validatedRuntimeDir(path, executableRel string) (string, error) {
	dir, err := validatedReadDir(path)
	if err != nil || dir == "" {
		return "", err
	}
	if executableRel == "" {
		return dir, nil
	}
	binaryPath := filepath.Join(dir, executableRel)
	if !integrationAgentExecCheck(binaryPath) {
		// Attempt to fix Homebrew tool permissions: some formulae install
		// with 0700/0600 which blocks the agent user. The invoker owns
		// these files and can grant read+execute via ACL without sudo.
		if repairHomebrewToolAccess(dir) && integrationAgentExecCheck(binaryPath) {
			return dir, nil
		}
		return "", fmt.Errorf("%q is not executable by %s", binaryPath, agentUser)
	}
	return dir, nil
}

func goRootFromPrefix(prefix string) string {
	for _, candidate := range []string{
		filepath.Join(prefix, "libexec"),
		prefix,
	} {
		if dir, err := validatedRuntimeDir(candidate, filepath.Join("bin", "go")); err == nil && dir != "" {
			return dir
		}
	}
	return ""
}

func (ctx *integrationResolveContext) brewPrefix(formulas ...string) brewPrefixResult {
	resolver := ctx.homebrewResolver()
	if resolver == nil {
		return brewPrefixResult{Detail: "Homebrew not found at canonical locations"}
	}

	allowed, detail := resolver.allowed()
	if !allowed {
		return brewPrefixResult{Detail: detail}
	}

	var probeError error
	for _, formula := range formulas {
		if dir := resolver.optPrefix(formula); dir != "" {
			return brewPrefixResult{
				Prefix:  dir,
				Formula: formula,
				Detail:  fmt.Sprintf("resolved via Homebrew opt/%s -> %s", formula, dir),
			}
		}

		out, err := ctx.Probe.Output(resolver.brewPath(), "--prefix", "--installed", formula)
		if err != nil || out == "" {
			if err != nil && probeError == nil && strings.Contains(err.Error(), "timed out after") {
				probeError = err
			}
			continue
		}
		dir, err := validatedReadDir(out)
		if err != nil || dir == "" {
			continue
		}
		return brewPrefixResult{
			Prefix:  dir,
			Formula: formula,
			Detail:  fmt.Sprintf("resolved via Homebrew %s -> %s", formula, dir),
		}
	}

	if probeError != nil {
		return brewPrefixResult{Detail: probeError.Error()}
	}
	return brewPrefixResult{Detail: "Homebrew fallback found no installed matching formula"}
}

func (ctx *integrationResolveContext) homebrewResolver() *integrationHomebrewResolver {
	if ctx.homebrew == nil {
		ctx.homebrew = &integrationHomebrewResolver{
			projectDir: ctx.ProjectDir,
			probe:      ctx.Probe,
		}
	}
	if ctx.homebrew.brewPath() == "" {
		return nil
	}
	return ctx.homebrew
}

func (r *integrationHomebrewResolver) brewPath() string {
	for _, candidate := range integrationBrewCandidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		return candidate
	}
	return ""
}

func (r *integrationHomebrewResolver) rootPrefix() string {
	brewPath := r.brewPath()
	if brewPath == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(brewPath), ".."))
}

func (r *integrationHomebrewResolver) optPrefix(formula string) string {
	root := r.rootPrefix()
	if root == "" {
		return ""
	}
	dir, err := validatedReadDir(filepath.Join(root, "opt", formula))
	if err != nil || dir == "" {
		return ""
	}
	return dir
}

func (r *integrationHomebrewResolver) allowed() (bool, string) {
	cfg, err := loadConfig()
	if err != nil {
		return false, "Homebrew fallback skipped: could not load config"
	}

	if allowed, configured := cfg.HomebrewIntegrationConsent(); configured {
		if allowed {
			return true, "Homebrew fallback enabled in config"
		}
		return false, "Homebrew fallback disabled in config"
	}

	decision, prompted := homebrewConsentPrompt()
	if !prompted {
		return false, "Homebrew fallback skipped: consent not configured; set hazmat config set integrations.homebrew enabled|disabled"
	}

	if err := setHomebrewIntegrationConsent(boolPtr(decision)); err != nil {
		fmt.Fprintf(os.Stderr, "hazmat: warning: could not save Homebrew integration consent: %v\n", err)
	}
	if decision {
		return true, "Homebrew fallback enabled and recorded in config"
	}
	return false, "Homebrew fallback disabled and recorded in config"
}

func (ctx *integrationResolveContext) resolveJavaHome() (string, string, error) {
	if javaHome := os.Getenv("JAVA_HOME"); javaHome != "" {
		dir, err := validatedJavaHome(javaHome)
		if err == nil && dir != "" {
			return dir, "tla-java (JAVA_HOME)", nil
		}
	}

	if output, err := ctx.Probe.Output("java", "-XshowSettings:properties", "-version"); err == nil && output != "" {
		if javaHome := parseJavaHome(output); javaHome != "" {
			dir, err := validatedJavaHome(javaHome)
			if err == nil && dir != "" {
				return dir, "tla-java (java runtime)", nil
			}
		}
	}

	if info, err := os.Stat(integrationJavaHomePath); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		if out, err := ctx.Probe.Output(integrationJavaHomePath); err == nil && out != "" {
			dir, err := validatedJavaHome(out)
			if err == nil && dir != "" {
				return dir, "tla-java (java_home)", nil
			}
		}
	}

	return "", "", nil
}

func parseJavaHome(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "java.home =") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "java.home ="))
	}
	return ""
}

func validatedJavaHome(path string) (string, error) {
	dir, err := validatedReadDir(path)
	if err != nil || dir == "" {
		return "", err
	}
	if javaLauncherStubHome(dir) {
		return "", fmt.Errorf("%q is the macOS launcher stub, not a real Java home", dir)
	}
	javaBin := filepath.Join(dir, "bin", "java")
	info, err := os.Stat(javaBin)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("%q is not a Java home", dir)
	}
	if !integrationAgentExecCheck(javaBin) {
		if repairHomebrewToolAccess(dir) && integrationAgentExecCheck(javaBin) {
			return dir, nil
		}
		return "", fmt.Errorf("%q is not executable by %s", javaBin, agentUser)
	}
	return dir, nil
}

func javaLauncherStubHome(dir string) bool {
	switch filepath.Clean(dir) {
	case "/usr", "/usr/bin":
		return true
	default:
		return false
	}
}

func shouldSetResolvedJavaHomeEnv() bool {
	javaHome := os.Getenv("JAVA_HOME")
	if javaHome == "" {
		return true
	}
	_, err := validatedJavaHome(javaHome)
	return err != nil
}

func javaHomeFromPrefix(prefix string) string {
	candidates := []string{
		filepath.Join(prefix, "libexec", "openjdk.jdk", "Contents", "Home"),
		prefix,
	}
	for _, candidate := range candidates {
		if dir, err := validatedJavaHome(candidate); err == nil && dir != "" {
			return dir
		}
	}
	return ""
}

func setHomebrewIntegrationConsent(value *bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Integrations.Homebrew = value
	return saveConfig(cfg)
}

func boolPtr(value bool) *bool {
	b := value
	return &b
}

func invokerGoModCache() string {
	probe := integrationProbeFactory()
	output, err := probe.Output("go", "env", "GOMODCACHE")
	if err != nil || output == "" {
		return ""
	}
	dir, err := validatedReadDir(output)
	if err != nil {
		return ""
	}
	return dir
}

func renderIntegrationDetails(details []string) string {
	if len(details) == 0 {
		return ""
	}

	var b bytes.Buffer
	fmt.Fprintln(&b, "hazmat: integration resolution")
	for _, detail := range details {
		fmt.Fprintf(&b, "  - %s\n", detail)
	}
	b.WriteByte('\n')
	return b.String()
}

func pathExecutableByAgent(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && os.IsPermission(pathErr) {
			return false
		}
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}

	agentInfo, err := user.Lookup(agentUser)
	if err != nil {
		return false
	}
	agentUID64, err := strconv.ParseUint(agentInfo.Uid, 10, 32)
	if err != nil {
		return false
	}
	agentUID := uint32(agentUID64)

	if !agentHasPathExecute(filepath.Dir(path), agentUID) {
		return false
	}

	groupHasAgent := false
	if group, err := user.LookupGroupId(strconv.FormatUint(uint64(stat.Gid), 10)); err == nil {
		groupHasAgent, _ = groupMembershipContains(group.Name, agentUser)
	}
	return executableByAgentMode(info.Mode(), stat.Uid, agentUID, groupHasAgent)
}

func agentHasPathExecute(path string, agentUID uint32) bool {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return false
	}
	for current := path; current != "/" && current != "."; current = filepath.Dir(current) {
		if current == os.Getenv("HOME") && homeAllowsAgentTraverse(current) {
			continue
		}
		info, err := os.Stat(current)
		if err != nil || !info.IsDir() {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return false
		}
		groupHasAgent := false
		if group, err := user.LookupGroupId(strconv.FormatUint(uint64(stat.Gid), 10)); err == nil {
			groupHasAgent, _ = groupMembershipContains(group.Name, agentUser)
		}
		if !executableByAgentMode(info.Mode(), stat.Uid, agentUID, groupHasAgent) {
			return false
		}
	}
	return true
}

func executableByAgentMode(mode os.FileMode, ownerUID, agentUID uint32, groupHasAgent bool) bool {
	perm := mode.Perm()
	if ownerUID == agentUID && perm&0o100 != 0 {
		return true
	}
	if groupHasAgent && perm&0o010 != 0 {
		return true
	}
	return perm&0o001 != 0
}

// ── Homebrew tool ACL repair ──────────────────────────────────────────────────

// repairHomebrewToolAccess grants the dev group read + execute access to a
// Homebrew Cellar directory tree. Some formulae (Go, golangci-lint) install
// with 0700/0600 permissions, blocking the agent user.
//
// The invoker owns these files, so no sudo is required for ACL modification.
// Only acts on paths under a known Homebrew prefix. Returns true if a fix
// was applied.
var repairHomebrewToolAccess = repairHomebrewToolAccessImpl

func repairHomebrewToolAccessImpl(dir string) bool {
	cellarRoot := homebrewCellarRoot(dir)
	if cellarRoot == "" {
		return false
	}

	// Only fix paths the invoker owns.
	info, err := os.Stat(cellarRoot)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	cu, err := user.Current()
	if err != nil {
		return false
	}
	uid, _ := strconv.ParseUint(cu.Uid, 10, 32)
	if stat.Uid != uint32(uid) {
		return false
	}

	// Use chmod o+rX on the formula parent (e.g., .../Cellar/go/) and
	// chmod -R o+rX on the version root (e.g., .../Cellar/go/1.26.1/).
	// Adds world-readable and world-executable (where owner already has +x).
	// This matches what Homebrew applies for most formulae; Go and
	// golangci-lint are outliers with 0700/0600. pathExecutableByAgent
	// checks Unix mode bits (not ACLs), so mode-bit repair is required.
	formulaDir := filepath.Dir(cellarRoot)
	if err := exec.Command("chmod", "o+rX", formulaDir).Run(); err != nil {
		return false
	}
	if err := exec.Command("chmod", "-R", "o+rX", cellarRoot).Run(); err != nil {
		return false
	}

	fmt.Fprintf(os.Stderr, "hazmat: fixed Homebrew tool permissions: %s\n", cellarRoot)
	return true
}

// homebrewCellarRoot returns the Cellar version root for a Homebrew path.
// e.g., /opt/homebrew/Cellar/go/1.26.1/libexec → /opt/homebrew/Cellar/go/1.26.1
func homebrewCellarRoot(path string) string {
	idx := strings.Index(path, "/Cellar/")
	if idx < 0 {
		return ""
	}
	cellarBase := path[:idx+len("/Cellar/")]
	rest := path[len(cellarBase):]
	parts := strings.SplitN(rest, "/", 3) // formula/version/...
	if len(parts) < 2 {
		return ""
	}
	root := filepath.Join(cellarBase, parts[0], parts[1])
	if _, err := os.Stat(root); err != nil {
		return ""
	}
	return root
}

func homebrewACLRepairTargets(cellarRoot, dir string) []string {
	seen := make(map[string]struct{})
	var targets []string
	add := func(path string) {
		path = filepath.Clean(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		targets = append(targets, path)
	}

	cleanDir := filepath.Clean(dir)
	add(cellarRoot)
	for path := cleanDir; ; path = filepath.Dir(path) {
		add(path)
		if path == cellarRoot || path == "/" || path == "." {
			break
		}
	}

	binDir := filepath.Join(cleanDir, "bin")
	if info, err := os.Stat(binDir); err == nil && info.IsDir() {
		add(binDir)
		if entries, err := os.ReadDir(binDir); err == nil {
			for _, entry := range entries {
				add(filepath.Join(binDir, entry.Name()))
			}
		}
	}

	return targets
}
