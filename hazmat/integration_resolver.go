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
	"node": {
		Summary:                  "active Node runtime probe with Homebrew node fallback",
		ReplacesDeclaredReadDirs: true,
		Resolve:                  resolveNodeIntegration,
	},
	"rust": {
		Summary:                  "rustc sysroot probe with Homebrew rust/rustup fallback",
		ReplacesDeclaredReadDirs: true,
		Resolve:                  resolveRustIntegration,
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
	for _, key := range []string{"LANG", "LC_ALL", "LC_CTYPE", "TERM"} {
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
		} else {
			result.Details = append(result.Details, fmt.Sprintf("go: Homebrew %s is installed, but %s cannot execute %s", brewResult.Formula, agentUser, filepath.Join(brewResult.Prefix, "libexec", "bin", "go")))
		}
	} else if brewResult.Detail != "" {
		result.Details = append(result.Details, "go: "+brewResult.Detail)
	}
	return result, nil
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
			if err != nil && probeError == nil {
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
