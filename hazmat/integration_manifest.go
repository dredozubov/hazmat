package hazmat

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hazmat/integrations"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var safeEnvKeys = integrations.SafeEnvKeys()

type IntegrationSpec = integrations.Spec

type IntegrationMeta = integrations.Meta

type IntegrationDetect = integrations.Detect

type IntegrationSession = integrations.Session

type IntegrationPlatformSession = integrations.PlatformSession

type IntegrationBackup = integrations.Backup

const (
	integrationMaxSize           = integrations.MaxSize
	integrationMaxReadDirs       = integrations.MaxReadDirs
	integrationMaxEnvKeys        = integrations.MaxEnvKeys
	integrationMaxExcludes       = integrations.MaxExcludes
	integrationMaxWarnings       = integrations.MaxWarnings
	integrationMaxCommands       = integrations.MaxCommands
	integrationMaxDetectFiles    = integrations.MaxDetectFiles
	integrationMaxDetectRootDirs = integrations.MaxDetectRootDirs
)

var integrationNameRe = integrations.NamePattern

func isCredentialGrantEnvKey(key string) bool {
	return integrations.IsCredentialGrantEnvKey(key)
}

func rejectCredentialGrantEnvKey(owner, field, key string) error {
	return integrations.RejectCredentialGrantEnvKey(owner, field, key)
}

func validateIntegrationSchema(p IntegrationSpec) error {
	return integrations.ValidateSchema(p)
}

func integrationSessionForCurrentPlatform(session IntegrationSession) IntegrationPlatformSession {
	return integrationSessionForPlatform(session, currentIntegrationPlatform())
}

func integrationSessionForPlatform(session IntegrationSession, platform string) IntegrationPlatformSession {
	return integrations.SessionForPlatform(session, platform)
}

// validateIntegrationPaths checks read_dirs against credential deny zones (V2).
// This runs at session start after tilde expansion and canonicalization.
// Returns the canonical paths for use in session merge.
func validateIntegrationPaths(p IntegrationSpec) ([]string, error) {
	return validateIntegrationPathsForPlatform(p, currentIntegrationPlatform())
}

func validateIntegrationPathsForPlatform(p IntegrationSpec, platform string) ([]string, error) {
	var canonical []string
	session := integrationSessionForPlatform(p.Session, platform)
	for _, dir := range session.ReadDirs {
		expanded := expandTilde(dir)

		// Path must exist (skip silently if it doesn't — same as defaultReadDirs).
		if _, err := os.Stat(expanded); err != nil {
			continue
		}

		resolved, err := canonicalizePath(expanded)
		if err != nil {
			continue // skip unresolvable paths
		}

		if isCredentialDenyPath(resolved) {
			return nil, fmt.Errorf("integration %q: read_dir %q resolves to credential deny zone %q",
				p.Meta.Name, dir, resolved)
		}
		if isHostStateDenyPath(resolved) {
			return nil, fmt.Errorf("integration %q: read_dir %q resolves to host-state deny zone %q",
				p.Meta.Name, dir, resolved)
		}

		canonical = append(canonical, resolved)
	}
	return canonical, nil
}

// ── Loading ────────────────────────────────────────────────────────────────

//go:embed integrations/*.yaml
var builtinIntegrationsFS embed.FS

const (
	repoRecommendedIntegrationsFile       = ".hazmat/integrations.yaml"
	legacyRepoRecommendedIntegrationsFile = ".hazmat/packs.yaml"
)

var (
	userIntegrationDir           = filepath.Join(os.Getenv("HOME"), ".hazmat/integrations")
	legacyUserIntegrationDir     = filepath.Join(os.Getenv("HOME"), ".hazmat/packs")
	integrationApprovalsFilePath = filepath.Join(os.Getenv("HOME"), ".hazmat/integration-approvals.yaml")
)

func hasLegacyTopLevelKey(data []byte, key string) bool {
	return integrations.HasLegacyTopLevelKey(data, key)
}

// loadIntegrationSpec parses and schema-validates a single integration manifest
// from YAML bytes. Unknown fields are rejected (fail closed) so typos and
// unsupported keys are caught at load time rather than silently ignored.
func loadIntegrationSpec(data []byte) (IntegrationSpec, error) {
	return integrations.LoadSpec(data)
}

// loadBuiltinIntegrationSpec loads a built-in integration spec by name from the
// embedded filesystem.
func loadBuiltinIntegrationSpec(name string) (IntegrationSpec, error) {
	data, err := builtinIntegrationsFS.ReadFile("integrations/" + name + ".yaml")
	if err != nil {
		return IntegrationSpec{}, fmt.Errorf("built-in integration %q not found", name)
	}
	return loadIntegrationSpec(data)
}

// loadUserIntegrationSpec loads a user-installed integration spec from
// ~/.hazmat/integrations/<name>.yaml.
func loadUserIntegrationSpec(name string) (IntegrationSpec, error) {
	path := filepath.Join(userIntegrationDir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		legacyPath := filepath.Join(legacyUserIntegrationDir, name+".yaml")
		if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
			return IntegrationSpec{}, fmt.Errorf("legacy user integration manifest %q detected; move it to %q", legacyPath, path)
		}
		return IntegrationSpec{}, fmt.Errorf("user integration %q: %w", name, err)
	}
	return loadIntegrationSpec(data)
}

// loadIntegrationSpecByName tries built-in first, then user-installed.
func loadIntegrationSpecByName(name string) (IntegrationSpec, error) {
	if p, err := loadBuiltinIntegrationSpec(name); err == nil {
		return p, nil
	}
	return loadUserIntegrationSpec(name)
}

// allBuiltinIntegrationNames returns the names of all embedded integrations.
func allBuiltinIntegrationNames() []string {
	entries, err := builtinIntegrationsFS.ReadDir("integrations")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".yaml") {
			names = append(names, strings.TrimSuffix(name, ".yaml"))
		}
	}
	return names
}

// ── Repo-recommended integrations ──────────────────────────────────────────
//
// A repo may declare recommended integrations in .hazmat/integrations.yaml.
// The file is pure data: a list of integration names referencing existing
// built-in or user-installed integration specs. No inline definitions, no
// paths, no env keys.
//
// Repo owns intent; host owns trust. Recommendations are never auto-activated.
// On first encounter (or after file change), hazmat prompts the user for
// approval. Approval is stored outside the repo in
// ~/.hazmat/integration-approvals.yaml, keyed by canonical project path +
// SHA-256 of the recommendations file.

// repoRecommendedIntegrations is the schema for .hazmat/integrations.yaml.
type repoRecommendedIntegrations struct {
	Integrations []string `yaml:"integrations"`
}

// integrationApprovalRecord is one entry in the approvals file.
type integrationApprovalRecord struct {
	ProjectDir string `yaml:"project"`
	FileHash   string `yaml:"hash"`
}

// integrationApprovalsFile is the top-level schema for
// ~/.hazmat/integration-approvals.yaml.
type integrationApprovalsFile struct {
	Approvals []integrationApprovalRecord `yaml:"approvals"`
}

// loadRepoRecommendations reads .hazmat/integrations.yaml from the project
// directory. Returns the integration names and the SHA-256 of the file
// contents. Returns nil names if the file doesn't exist.
func loadRepoRecommendations(projectDir string) ([]string, string, error) {
	legacyPath := filepath.Join(projectDir, legacyRepoRecommendedIntegrationsFile)
	if _, err := os.Stat(legacyPath); err == nil {
		return nil, "", fmt.Errorf("legacy repo integration file detected: rename %s to %s", legacyRepoRecommendedIntegrationsFile, repoRecommendedIntegrationsFile)
	}

	path := filepath.Join(projectDir, repoRecommendedIntegrationsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("read %s: %w", repoRecommendedIntegrationsFile, err)
	}

	if len(data) > integrationMaxSize {
		return nil, "", fmt.Errorf("%s exceeds %d byte limit", repoRecommendedIntegrationsFile, integrationMaxSize)
	}

	if hasLegacyTopLevelKey(data, "packs") {
		return nil, "", fmt.Errorf("%s uses legacy 'packs:' schema; rename the key to 'integrations:'", repoRecommendedIntegrationsFile)
	}

	var rec repoRecommendedIntegrations
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&rec); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", repoRecommendedIntegrationsFile, err)
	}

	// Validate: every name must resolve through existing loaders.
	for _, name := range rec.Integrations {
		if _, err := loadIntegrationSpecByName(name); err != nil {
			return nil, "", fmt.Errorf("%s: unknown integration %q; see %s", repoRecommendedIntegrationsFile, name, integrationContributorFlowDocURL)
		}
	}

	hash := sha256.Sum256(data)
	return rec.Integrations, hex.EncodeToString(hash[:]), nil
}

// loadIntegrationApprovals reads the approval file.
func loadIntegrationApprovals() integrationApprovalsFile {
	data, err := os.ReadFile(integrationApprovalsFilePath)
	if err != nil {
		return integrationApprovalsFile{}
	}
	var af integrationApprovalsFile
	_ = yaml.Unmarshal(data, &af)
	return af
}

// saveIntegrationApprovals writes the approval file.
func saveIntegrationApprovals(af integrationApprovalsFile) error {
	dir := filepath.Dir(integrationApprovalsFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(&af)
	if err != nil {
		return err
	}
	return os.WriteFile(integrationApprovalsFilePath, data, 0o600)
}

// isApproved checks whether the given project + hash combination is approved.
func isApproved(projectDir, fileHash string) bool {
	af := loadIntegrationApprovals()
	for _, rec := range af.Approvals {
		if rec.ProjectDir == projectDir && rec.FileHash == fileHash {
			return true
		}
	}
	return false
}

// recordApproval stores approval for a project + hash. Replaces any existing
// approval for the same project (since hash changed = re-approve).
func recordApproval(projectDir, fileHash string) error {
	af := loadIntegrationApprovals()

	// Remove stale approval for same project.
	filtered := af.Approvals[:0]
	for _, rec := range af.Approvals {
		if rec.ProjectDir != projectDir {
			filtered = append(filtered, rec)
		}
	}
	filtered = append(filtered, integrationApprovalRecord{
		ProjectDir: projectDir,
		FileHash:   fileHash,
	})
	af.Approvals = filtered

	return saveIntegrationApprovals(af)
}

// ── Resolution: CLI flags + config pinning + repo recommendations ────────

// resolveActiveIntegrations determines which integrations to load for a session.
// Sources (in priority order):
//  1. --integration CLI flags (always active, no approval needed)
//  2. Config pinning for the project (always active, user configured)
//  3. Repo .hazmat/integrations.yaml when already approved in host-owned state
//
// Returns loaded, validated integration specs.
func resolveActiveIntegrations(integrationFlags []string, projectDir string) ([]IntegrationSpec, error) {
	// Collect integration names from CLI flags.
	names := make(map[string]struct{})
	for _, n := range integrationFlags {
		names[n] = struct{}{}
	}

	// Add pinned integrations from config if not already specified via CLI.
	cfg, _ := loadConfig()
	for _, pin := range cfg.PinnedIntegrations() {
		pinned, err := canonicalizePath(expandTilde(pin.ProjectDir))
		if err != nil {
			continue
		}
		if pinned == projectDir {
			for _, n := range pin.Integrations {
				names[n] = struct{}{}
			}
		}
	}

	// Add repo-recommended integrations if approved by host.
	//
	// Unapproved recommendations are surfaced through repo setup suggestions
	// instead of a separate integration-name approval prompt.
	if recNames, fileHash, err := loadRepoRecommendations(projectDir); err == nil && len(recNames) > 0 {
		if isApproved(projectDir, fileHash) {
			for _, n := range recNames {
				names[n] = struct{}{}
			}
		}
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "hazmat: warning: %v\n", err)
	}

	if len(names) == 0 {
		return nil, nil
	}

	orderedNames := make([]string, 0, len(names))
	for name := range names {
		orderedNames = append(orderedNames, name)
	}
	sort.Strings(orderedNames)

	var integrations []IntegrationSpec
	for _, name := range orderedNames {
		spec, err := loadIntegrationSpecByName(name)
		if err != nil {
			return nil, err
		}
		integrations = append(integrations, spec)
	}
	return integrations, nil
}

// ── Detection / suggestion ─────────────────────────────────────────────────

var integrationDetectIgnoredDirs = map[string]struct{}{
	".beads":       {},
	".git":         {},
	".next":        {},
	".nuxt":        {},
	".terraform":   {},
	".turbo":       {},
	".venv":        {},
	"build":        {},
	"dist":         {},
	"node_modules": {},
	"target":       {},
	"vendor":       {},
	"venv":         {},
}

var integrationDetectAuxiliaryTopLevelDirs = map[string]struct{}{
	".github":    {},
	"benchmarks": {},
	"docs":       {},
	"docs-site":  {},
	"examples":   {},
	"fixtures":   {},
	"fuzz":       {},
	"playground": {},
	"scripts":    {},
	"test":       {},
	"tests":      {},
}

var integrationDetectPreferredTopLevelDirs = map[string]map[string]struct{}{
	"node": {
		"app":      {},
		"apps":     {},
		"client":   {},
		"frontend": {},
		"site":     {},
		"ui":       {},
		"web":      {},
	},
	"python-pip": {
		"api":     {},
		"app":     {},
		"apps":    {},
		"backend": {},
		"server":  {},
		"service": {},
		"worker":  {},
	},
	"python-poetry": {
		"api":     {},
		"app":     {},
		"apps":    {},
		"backend": {},
		"server":  {},
		"service": {},
		"worker":  {},
	},
	"python-uv": {
		"api":     {},
		"app":     {},
		"apps":    {},
		"backend": {},
		"server":  {},
		"service": {},
		"worker":  {},
	},
	"rust": {
		"compiler": {},
		"crates":   {},
		"engine":   {},
		"native":   {},
		"rust":     {},
	},
}

const integrationDetectMaxDepth = 4

type projectDetectFile struct {
	Rel   string
	Name  string
	Dir   string
	Depth int
}

type projectDetectIndex struct {
	RootDirs map[string]struct{}
	RootFile []string
	Files    []projectDetectFile
	ByDir    map[string][]string
}

func buildProjectDetectIndex(projectDir string) projectDetectIndex {
	index := projectDetectIndex{
		RootDirs: make(map[string]struct{}),
		ByDir:    make(map[string][]string),
	}

	filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error { //nolint:errcheck // best-effort suggestion probe
		if err != nil || path == projectDir {
			return nil
		}

		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		name := d.Name()
		if d.IsDir() {
			if depth == 1 {
				index.RootDirs[name] = struct{}{}
			}
			if depth > integrationDetectMaxDepth {
				return filepath.SkipDir
			}
			if _, skip := integrationDetectIgnoredDirs[name]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > integrationDetectMaxDepth {
			return nil
		}

		dir := filepath.Dir(rel)
		if dir == "." {
			dir = ""
			index.RootFile = append(index.RootFile, name)
		}
		index.Files = append(index.Files, projectDetectFile{
			Rel:   rel,
			Name:  name,
			Dir:   dir,
			Depth: depth,
		})
		index.ByDir[dir] = append(index.ByDir[dir], name)
		return nil
	})

	return index
}

func (index projectDetectIndex) hasRootDir(name string) bool {
	if index.RootDirs == nil {
		return false
	}
	_, ok := index.RootDirs[name]
	return ok
}

func (index projectDetectIndex) rootMatchesDetectFile(pattern string) bool {
	if pattern == "" {
		return false
	}
	for _, name := range index.RootFile {
		if detectFileMatches(pattern, name) {
			return true
		}
	}
	return false
}

func (index projectDetectIndex) matchesDetectFile(integrationName, pattern string) bool {
	if pattern == "" {
		return false
	}
	for _, file := range index.Files {
		if !detectFileMatches(pattern, file.Name) {
			continue
		}
		if file.Depth > 1 && !integrationSuggestsFromNestedPath(integrationName, file.Rel) {
			continue
		}
		return true
	}
	return false
}

func (index projectDetectIndex) hasFileWithSiblingPattern(pattern, siblingPattern string) bool {
	if pattern == "" || siblingPattern == "" {
		return false
	}
	for _, file := range index.Files {
		if !detectFileMatches(pattern, file.Name) {
			continue
		}
		for _, sibling := range index.ByDir[file.Dir] {
			if detectFileMatches(siblingPattern, sibling) {
				return true
			}
		}
	}
	return false
}

func detectPatternHasWildcard(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func detectFileMatches(pattern, name string) bool {
	if !detectPatternHasWildcard(pattern) {
		return pattern == name
	}
	matched, err := filepath.Match(pattern, name)
	return err == nil && matched
}

func integrationSuggestsFromNestedPath(integrationName, rel string) bool {
	if rel == "" {
		return false
	}
	topLevel := strings.Split(rel, string(os.PathSeparator))[0]
	if _, skip := integrationDetectAuxiliaryTopLevelDirs[topLevel]; skip {
		return false
	}
	preferred, ok := integrationDetectPreferredTopLevelDirs[integrationName]
	if !ok {
		return true
	}
	_, allowed := preferred[topLevel]
	return allowed
}

func integrationSuggestionMatchesIndex(index projectDetectIndex, spec IntegrationSpec) bool {
	for _, d := range spec.Detect.RootDirs {
		if index.hasRootDir(d) {
			return true
		}
	}
	switch spec.Meta.Name {
	case "java-gradle", "java-maven":
		for _, f := range spec.Detect.Files {
			if index.rootMatchesDetectFile(f) {
				return true
			}
		}
		return false
	case "tla-java":
		return index.hasFileWithSiblingPattern("*.cfg", "*.tla")
	case "python-pip":
		// Defer to python-uv / python-poetry when their lock files are present
		// at root; otherwise suggest python-pip from any requirements.txt match.
		for _, lock := range []string{"uv.lock", "poetry.lock"} {
			if index.rootMatchesDetectFile(lock) {
				return false
			}
		}
		for _, f := range spec.Detect.Files {
			if index.matchesDetectFile(spec.Meta.Name, f) {
				return true
			}
		}
		return false
	default:
		for _, f := range spec.Detect.Files {
			if index.matchesDetectFile(spec.Meta.Name, f) {
				return true
			}
		}
		return false
	}
}

// suggestIntegrations checks detect.files against the project directory and returns
// names of built-in integrations that match but are not already active.
func suggestIntegrations(projectDir string, activeNames map[string]struct{}) []string {
	var suggestions []string
	var candidates []IntegrationSpec
	for _, name := range allBuiltinIntegrationNames() {
		if _, active := activeNames[name]; active {
			continue
		}
		p, err := loadBuiltinIntegrationSpec(name)
		if err != nil {
			continue
		}
		if len(p.Detect.Files) == 0 && len(p.Detect.RootDirs) == 0 {
			continue
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return nil
	}

	index := buildProjectDetectIndex(projectDir)
	for _, p := range candidates {
		if integrationSuggestionMatchesIndex(index, p) {
			suggestions = append(suggestions, p.Meta.Name)
		}
	}
	return suggestions
}

// ── Session merge ──────────────────────────────────────────────────────────

// integrationMergeResult holds the merged output of all active integrations, ready for
// injection into session setup.
type integrationMergeResult = integrations.MergeResult

// mergeIntegrations validates paths and merges all active integrations into a
// single result.
func mergeIntegrations(integrations []IntegrationSpec) (integrationMergeResult, error) {
	resolved := make([]resolvedIntegration, 0, len(integrations))
	for _, spec := range integrations {
		resolved = append(resolved, resolvedIntegration{Spec: spec})
	}
	return mergeResolvedIntegrations(resolved)
}

func mergeResolvedIntegrations(resolved []resolvedIntegration) (integrationMergeResult, error) {
	return mergeResolvedIntegrationsForPlatform(resolved, currentIntegrationPlatform())
}

func mergeResolvedIntegrationsForPlatform(resolved []resolvedIntegration, platform string) (integrationMergeResult, error) {
	items := make([]integrations.Resolved, 0, len(resolved))
	for _, integration := range resolved {
		items = append(items, integrations.Resolved{
			Spec:                    integration.Spec,
			ReplaceDeclaredReadDirs: integration.ReplaceDeclaredReadDirs,
			AdditionalReadDirs:      integration.AdditionalReadDirs,
			ResolvedEnv:             integration.ResolvedEnv,
			AdditionalWarnings:      integration.AdditionalWarnings,
		})
	}

	return integrations.MergeResolved(items, integrations.MergeOptions{
		Platform:         platform,
		ValidateReadDirs: validateIntegrationPathsForPlatform,
		Getenv:           os.Getenv,
	})
}

// ── CLI command ────────────────────────────────────────────────────────────

const (
	integrationDocsURL               = "https://github.com/dredozubov/hazmat/blob/master/docs/integrations.md"
	integrationContributorFlowDocURL = "https://github.com/dredozubov/hazmat/blob/master/docs/integration-contributor-flow.md"
)

func newIntegrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "List and inspect session integrations",
		Long: `Session integrations configure session ergonomics for technology stacks.

They set read-only paths, backup excludes, and env passthrough for common
development environments. Integrations cannot widen trust boundaries — they may
only reduce friction or tighten defaults.

  hazmat integration list        List available integrations
  hazmat integration show <name> Show integration details
  hazmat integration setup       Show guided setup and contribution paths
  hazmat integration scaffold <name>
                                Create a draft integration manifest
  hazmat integration validate <file-or-name>
                                Validate an integration manifest
  hazmat integration rejections  Inspect or clear persisted rejected suggestions

Learn integrations: ` + integrationDocsURL + `
Missing stack support? See ` + integrationContributorFlowDocURL,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runIntegrationList()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available session integrations",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runIntegrationList()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show details of a session integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runIntegrationShow(args[0])
		},
	})
	cmd.AddCommand(
		newIntegrationSetupCmd(),
		newIntegrationScaffoldCmd(),
		newIntegrationValidateCmd(),
		newIntegrationRejectionsCmd(),
	)

	return cmd
}

func newIntegrationRejectionsCmd() *cobra.Command {
	var project string

	cmd := &cobra.Command{
		Use:   "rejections",
		Short: "Inspect or clear persisted rejected suggested integrations",
		Long: `Rejected suggested integrations are remembered per project so Hazmat
does not keep re-prompting during interactive launches. Use these commands
to inspect the saved rejected suggestions for a project, or clear them so
Hazmat will ask again on a future launch.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List persisted rejected suggested integrations",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runIntegrationRejectionsList(project)
		},
	}
	listCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory to inspect (defaults to all configured projects)")

	clearCmd := &cobra.Command{
		Use:   "clear [integration-name ...]",
		Short: "Clear persisted rejected suggested integrations for a project",
		Long: `Clear all rejected suggested integrations for a project, or only the
named integrations when arguments are provided. Clearing them lets Hazmat
ask again on future interactive launches.`,
		RunE: func(_ *cobra.Command, args []string) error {
			return runIntegrationRejectionsClear(project, args)
		},
	}
	clearCmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory whose rejected suggestions should be cleared")

	cmd.AddCommand(listCmd, clearCmd)
	return cmd
}

func runIntegrationList() error {
	fmt.Println()
	fmt.Println("  Built-in integrations:")
	fmt.Println()
	for _, name := range allBuiltinIntegrationNames() {
		spec, err := loadBuiltinIntegrationSpec(name)
		if err != nil {
			continue
		}
		desc := spec.Meta.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("    %-20s %s\n", name, desc)
	}

	// List user integrations.
	entries, err := os.ReadDir(userIntegrationDir)
	if err == nil {
		var userIntegrations []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				userIntegrations = append(userIntegrations, strings.TrimSuffix(e.Name(), ".yaml"))
			}
		}
		if len(userIntegrations) > 0 {
			fmt.Println()
			fmt.Printf("  User integrations (%s):\n", userIntegrationDir)
			fmt.Println()
			for _, name := range userIntegrations {
				spec, err := loadUserIntegrationSpec(name)
				if err != nil {
					fmt.Printf("    %-20s (load error: %v)\n", name, err)
					continue
				}
				desc := spec.Meta.Description
				if desc == "" {
					desc = "(no description)"
				}
				fmt.Printf("    %-20s %s\n", name, desc)
			}
		}
	}

	// Show pinned projects.
	cfg, _ := loadConfig()
	if pins := cfg.PinnedIntegrations(); len(pins) > 0 {
		fmt.Println()
		fmt.Println("  Pinned projects:")
		fmt.Println()
		for _, pin := range pins {
			fmt.Printf("    %-30s %s\n", pin.ProjectDir, strings.Join(pin.Integrations, ", "))
		}
	}

	fmt.Println()
	fmt.Println("  Setup:    hazmat integration setup")
	fmt.Println("  Activate: hazmat claude|codex|opencode|shell|exec --integration <name>")
	fmt.Println("  Pin:      hazmat config set integrations.pin \"~/workspace/app:node,go\"")
	fmt.Println("  Draft:    hazmat integration scaffold <name> --from-current-project")
	fmt.Println("  Validate: hazmat integration validate <file-or-name>")
	fmt.Println("  Prompt:   interactive harness launches can approve suggested integrations automatically")
	fmt.Printf("  Learn:    %s\n", integrationDocsURL)
	fmt.Printf("  Contribute: missing your stack? %s\n", integrationContributorFlowDocURL)
	fmt.Println()
	return nil
}

func runIntegrationRejectionsList(project string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if project != "" {
		projectDir, err := resolveDir(project, true)
		if err != nil {
			return err
		}
		rejected := cfg.ProjectRejectedIntegrations(projectDir)
		if len(rejected) == 0 {
			fmt.Printf("No rejected suggested integrations recorded for %s\n", projectDir)
			return nil
		}
		fmt.Printf("Rejected suggested integrations for %s:\n", projectDir)
		for _, name := range rejected {
			fmt.Printf("  - %s\n", name)
		}
		return nil
	}

	records := cfg.RejectedIntegrations()
	if len(records) == 0 {
		fmt.Println("No rejected suggested integrations recorded.")
		return nil
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].ProjectDir < records[j].ProjectDir
	})
	fmt.Println("Rejected suggested integrations:")
	for _, record := range records {
		fmt.Printf("  %s: %s\n", record.ProjectDir, strings.Join(record.Integrations, ", "))
	}
	return nil
}

func runIntegrationRejectionsClear(project string, names []string) error {
	if strings.TrimSpace(project) == "" {
		return fmt.Errorf("project directory is required (use -C/--project)")
	}

	projectDir, err := resolveDir(project, true)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	current := cfg.ProjectRejectedIntegrations(projectDir)
	if len(current) == 0 {
		fmt.Printf("No rejected suggested integrations recorded for %s\n", projectDir)
		return nil
	}

	if len(names) == 0 {
		cfg.Integrations.Rejected = upsertRejectedIntegrations(cfg.Integrations.Rejected, projectDir, nil)
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Cleared all rejected suggested integrations for %s\n", projectDir)
		return nil
	}

	currentSet := stringSet(current)
	toRemove := stringSet(names)
	var unknown []string
	for name := range toRemove {
		if _, ok := currentSet[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("project %s has no rejected suggested integrations named: %s", projectDir, strings.Join(unknown, ", "))
	}

	remaining := filterStrings(current, toRemove)
	cfg.Integrations.Rejected = upsertRejectedIntegrations(cfg.Integrations.Rejected, projectDir, remaining)
	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Cleared rejected suggested integrations for %s: %s\n", projectDir, strings.Join(dedupeStrings(names), ", "))
	return nil
}

func runIntegrationShow(name string) error {
	spec, err := loadIntegrationSpecByName(name)
	if err != nil {
		return err
	}

	projectDir, err := os.Getwd()
	if err != nil {
		projectDir = "."
	}
	resolved := resolvedIntegration{Spec: spec}
	if resolvedSet, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{spec}); err == nil && len(resolvedSet) == 1 {
		resolved = resolvedSet[0]
	}
	merged, err := mergeResolvedIntegrations([]resolvedIntegration{resolved})
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  Integration: %s\n", spec.Meta.Name)
	if spec.Meta.Description != "" {
		fmt.Printf("  %s\n", spec.Meta.Description)
	}
	fmt.Println()

	if len(spec.Detect.Files) > 0 {
		fmt.Printf("  Detect:          %s\n", strings.Join(spec.Detect.Files, ", "))
	}
	if len(spec.Detect.RootDirs) > 0 {
		var labeled []string
		for _, d := range spec.Detect.RootDirs {
			labeled = append(labeled, d+"/")
		}
		fmt.Printf("  Detect root dirs: %s\n", strings.Join(labeled, ", "))
	}
	if spec, ok := integrationResolverFor(name); ok {
		fmt.Printf("  Resolver:        %s\n", spec.Summary)
	}
	effectiveSession := integrationSessionForCurrentPlatform(spec.Session)
	platform := currentIntegrationPlatform()
	if len(effectiveSession.ReadDirs) > 0 {
		fmt.Printf("  Declared read dirs (%s): %s\n", platform, strings.Join(effectiveSession.ReadDirs, ", "))
	}
	if len(merged.ReadDirs) > 0 {
		fmt.Printf("  Resolved read dirs: %s\n", strings.Join(merged.ReadDirs, ", "))
	}
	if len(effectiveSession.EnvPassthrough) > 0 {
		fmt.Printf("  Env passthrough (%s): %s\n", platform, strings.Join(effectiveSession.EnvPassthrough, ", "))
	}
	if len(merged.EnvPassthrough) > 0 {
		var envPairs []string
		for key, value := range merged.EnvPassthrough {
			envPairs = append(envPairs, key+"="+value)
		}
		sort.Strings(envPairs)
		fmt.Printf("  Resolved env:    %s\n", strings.Join(envPairs, ", "))
	}
	if len(spec.Backup.Excludes) > 0 {
		fmt.Printf("  Excludes:        %s\n", strings.Join(spec.Backup.Excludes, ", "))
	}
	if resolved.Source != "" {
		fmt.Printf("  Source:          %s\n", resolved.Source)
	}
	if len(resolved.Details) > 0 {
		fmt.Println()
		fmt.Println("  Resolution:")
		for _, detail := range resolved.Details {
			fmt.Printf("    - %s\n", detail)
		}
	}
	if len(spec.Warnings) > 0 {
		fmt.Println()
		fmt.Println("  Warnings:")
		for _, w := range spec.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}
	if len(spec.Commands) > 0 {
		fmt.Println()
		fmt.Println("  Commands:")
		for name, cmd := range spec.Commands {
			fmt.Printf("    %-12s %s\n", name, cmd)
		}
	}
	fmt.Println()
	return nil
}
