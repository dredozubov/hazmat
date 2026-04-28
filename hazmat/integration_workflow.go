package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type integrationSetupOptions struct {
	Project   string
	Recommend string
}

type integrationScaffoldOptions struct {
	Project            string
	Output             string
	User               bool
	Force              bool
	FromCurrentProject bool
}

type integrationValidateOptions struct {
	Project string
}

type integrationScaffoldEvidence struct {
	DetectFiles    []string
	BackupExcludes []string
}

type integrationScaffoldYAML struct {
	Integration IntegrationMeta     `yaml:"integration"`
	Detect      *IntegrationDetect  `yaml:"detect,omitempty"`
	Session     *IntegrationSession `yaml:"session,omitempty"`
	Backup      *IntegrationBackup  `yaml:"backup,omitempty"`
	Warnings    []string            `yaml:"warnings,omitempty"`
	Commands    map[string]string   `yaml:"commands,omitempty"`
}

const integrationScaffoldMaxEvidence = 10

func newIntegrationSetupCmd() *cobra.Command {
	var opts integrationSetupOptions

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Show guided integration setup paths",
		Long: `Show the integration doorway for the current project.

Without flags, this prints the existing integrations Hazmat can use, any
currently suggested built-ins, and the commands for recommending or creating
integration manifests.

Use --recommend to write .hazmat/integrations.yaml with existing integration
names. This only references known built-in or user-installed manifests; it does
not define custom paths inline.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runIntegrationSetup(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Project, "project", "C", "",
		"Project directory to inspect (defaults to current directory)")
	cmd.Flags().StringVar(&opts.Recommend, "recommend", "",
		"Write .hazmat/integrations.yaml with comma- or space-separated integration names")
	return cmd
}

func newIntegrationScaffoldCmd() *cobra.Command {
	opts := integrationScaffoldOptions{FromCurrentProject: true}

	cmd := &cobra.Command{
		Use:   "scaffold <name>",
		Short: "Create a draft integration manifest",
		Long: `Create a draft integration manifest from user intent.

Hazmat does not try to identify every possible ecosystem. The name comes from
you; --from-current-project only collects candidate detect markers and snapshot
excludes from the project so the draft starts from local evidence.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runIntegrationScaffold(args[0], opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Project, "project", "C", "",
		"Project directory to inspect for draft evidence (defaults to current directory)")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "",
		"Output manifest path (defaults to built-in dir in Hazmat source, otherwise ~/.hazmat/integrations)")
	cmd.Flags().BoolVar(&opts.User, "user", false,
		"Write to ~/.hazmat/integrations/<name>.yaml")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false,
		"Overwrite an existing manifest")
	cmd.Flags().BoolVar(&opts.FromCurrentProject, "from-current-project", true,
		"Collect candidate detect markers and excludes from the project")
	return cmd
}

func newIntegrationValidateCmd() *cobra.Command {
	var opts integrationValidateOptions

	cmd := &cobra.Command{
		Use:   "validate <file-or-name>",
		Short: "Validate an integration manifest",
		Long: `Validate a built-in, user-installed, or file-backed integration manifest.

Validation checks the strict manifest schema, safe environment passthrough
allowlist, size and count bounds, and existing read-only paths against Hazmat's
credential deny zones.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runIntegrationValidate(args[0], opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Project, "project", "C", "",
		"Project directory used only for display context (defaults to current directory)")
	return cmd
}

func runIntegrationSetup(opts integrationSetupOptions) error {
	projectDir, err := integrationWorkflowProjectDir(opts.Project)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.Recommend) != "" {
		return runIntegrationSetupRecommend(projectDir, opts.Recommend)
	}

	active, err := resolveActiveIntegrations(nil, projectDir)
	if err != nil {
		return err
	}
	activeNames := make([]string, 0, len(active))
	for _, spec := range active {
		activeNames = append(activeNames, spec.Meta.Name)
	}
	sort.Strings(activeNames)
	suggestions := suggestedIntegrationsForProject(projectDir, stringSet(activeNames))
	builtins := allBuiltinIntegrationNames()
	sort.Strings(builtins)

	fmt.Println()
	fmt.Println("hazmat: integration setup")
	fmt.Printf("  Project:              %s\n", projectDir)
	fmt.Printf("  Active integrations:  %s\n", sessionContractList(activeNames))
	fmt.Printf("  Suggested built-ins:  %s\n", sessionContractList(suggestions))
	fmt.Printf("  Available built-ins:  %s\n", strings.Join(builtins, ", "))
	fmt.Println()
	fmt.Println("  Use existing once:    hazmat claude --integration <name>")
	fmt.Printf("  Pin for project:      hazmat config set integrations.pin %q\n", projectDir+":<name>")
	fmt.Println("  Recommend in repo:    hazmat integration setup --recommend <name[,name]>")
	fmt.Println("  Create draft:         hazmat integration scaffold <name> --from-current-project")
	fmt.Println("  Validate draft:       hazmat integration validate <file-or-name>")
	fmt.Printf("  Learn integrations:   %s\n", integrationDocsURL)
	fmt.Println()
	return nil
}

func runIntegrationSetupRecommend(projectDir, rawNames string) error {
	names := parseIntegrationNameList(rawNames)
	if len(names) == 0 {
		return fmt.Errorf("at least one integration name is required")
	}
	for _, name := range names {
		if _, err := loadIntegrationSpecByName(name); err != nil {
			return fmt.Errorf("unknown integration %q; run 'hazmat integration list' or create a draft with 'hazmat integration scaffold %s'", name, name)
		}
	}

	path := filepath.Join(projectDir, repoRecommendedIntegrationsFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := yaml.Marshal(repoRecommendedIntegrations{Integrations: names})
	if err != nil {
		return fmt.Errorf("render %s: %w", repoRecommendedIntegrationsFile, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Printf("Wrote %s\n", path)
	fmt.Printf("Recommended integrations: %s\n", strings.Join(names, ", "))
	fmt.Println("Hazmat will ask the host user to approve this repo recommendation on first use.")
	return nil
}

func runIntegrationScaffold(name string, opts integrationScaffoldOptions) error {
	name = strings.TrimSpace(name)
	if !integrationNameRe.MatchString(name) {
		return fmt.Errorf("integration name %q must match %s", name, integrationNameRe)
	}

	var evidence integrationScaffoldEvidence
	var projectDir string
	var err error
	if opts.FromCurrentProject {
		projectDir, err = integrationWorkflowProjectDir(opts.Project)
		if err != nil {
			return err
		}
		evidence = collectIntegrationScaffoldEvidence(projectDir)
	}

	outputPath, err := integrationScaffoldOutputPath(name, opts)
	if err != nil {
		return err
	}
	if _, err := os.Stat(outputPath); err == nil && !opts.Force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", outputPath)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", outputPath, err)
	}

	data, spec, err := renderIntegrationScaffold(name, evidence)
	if err != nil {
		return err
	}
	if _, err := loadIntegrationSpec(data); err != nil {
		return fmt.Errorf("generated invalid integration manifest: %w", err)
	}

	mode := os.FileMode(0o755)
	if isUserIntegrationOutput(outputPath) {
		mode = 0o700
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), mode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(outputPath), err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}

	fmt.Printf("Created integration draft: %s\n", outputPath)
	if projectDir != "" {
		fmt.Printf("Project evidence: %s\n", projectDir)
	}
	fmt.Printf("Detect markers: %s\n", sessionContractList(spec.Detect.Files))
	fmt.Printf("Snapshot excludes: %s\n", sessionContractList(spec.Backup.Excludes))
	fmt.Println()
	fmt.Printf("Next: edit %s\n", outputPath)
	fmt.Printf("Then: hazmat integration validate %s\n", outputPath)
	fmt.Printf("Contributor flow: %s\n", integrationContributorFlowDocURL)
	fmt.Println("For a built-in PR, keep the manifest small and include a focused test or docs note when behavior changes.")
	return nil
}

func runIntegrationValidate(ref string, opts integrationValidateOptions) error {
	projectDir, err := integrationWorkflowProjectDir(opts.Project)
	if err != nil {
		return err
	}
	spec, source, err := loadIntegrationSpecForValidation(ref)
	if err != nil {
		return err
	}
	if _, err := validateIntegrationPaths(spec); err != nil {
		return err
	}

	fmt.Printf("Integration manifest valid: %s\n", spec.Meta.Name)
	fmt.Printf("Source: %s\n", source)
	fmt.Printf("Project context: %s\n", projectDir)
	fmt.Printf("Platform: %s\n", currentIntegrationPlatform())
	return nil
}

func integrationWorkflowProjectDir(project string) (string, error) {
	return resolveDir(project, true)
}

func parseIntegrationNameList(raw string) []string {
	return dedupeStrings(strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}))
}

func integrationScaffoldOutputPath(name string, opts integrationScaffoldOptions) (string, error) {
	if strings.TrimSpace(opts.Output) != "" {
		return filepath.Abs(expandTilde(opts.Output))
	}
	if opts.User {
		return filepath.Join(userIntegrationDir, name+".yaml"), nil
	}
	if wd, err := os.Getwd(); err == nil {
		if dir, ok := findBuiltinIntegrationOutputDir(wd); ok {
			return filepath.Join(dir, name+".yaml"), nil
		}
	}
	return filepath.Join(userIntegrationDir, name+".yaml"), nil
}

func findBuiltinIntegrationOutputDir(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, "hazmat", "integrations")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isUserIntegrationOutput(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	userDir, err := filepath.Abs(userIntegrationDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(userDir, abs)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func renderIntegrationScaffold(name string, evidence integrationScaffoldEvidence) ([]byte, IntegrationSpec, error) {
	spec := IntegrationSpec{
		Meta: IntegrationMeta{
			Name:        name,
			Version:     1,
			Description: name + " integration draft",
		},
		Detect: IntegrationDetect{
			Files: evidence.DetectFiles,
		},
		Session: IntegrationSession{},
		Backup: IntegrationBackup{
			Excludes: evidence.BackupExcludes,
		},
	}

	doc := integrationScaffoldYAML{
		Integration: spec.Meta,
		Session:     &IntegrationSession{},
	}
	if len(spec.Detect.Files) > 0 || len(spec.Detect.RootDirs) > 0 {
		doc.Detect = &spec.Detect
	}
	if len(spec.Backup.Excludes) > 0 {
		doc.Backup = &spec.Backup
	}
	data, err := yaml.Marshal(doc)
	return data, spec, err
}

func loadIntegrationSpecForValidation(ref string) (IntegrationSpec, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return IntegrationSpec{}, "", fmt.Errorf("integration name or manifest path is required")
	}
	if spec, path, ok, err := loadIntegrationSpecFromPath(ref); ok || err != nil {
		return spec, path, err
	}
	spec, err := loadIntegrationSpecByName(ref)
	if err != nil {
		return IntegrationSpec{}, "", err
	}
	return spec, ref, nil
}

func loadIntegrationSpecFromPath(ref string) (IntegrationSpec, string, bool, error) {
	path := expandTilde(ref)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return IntegrationSpec{}, "", false, nil
		}
		return IntegrationSpec{}, path, true, fmt.Errorf("stat %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return IntegrationSpec{}, path, true, fmt.Errorf("read %s: %w", path, err)
	}
	spec, err := loadIntegrationSpec(data)
	if err != nil {
		return IntegrationSpec{}, path, true, err
	}
	return spec, path, true, nil
}

func collectIntegrationScaffoldEvidence(projectDir string) integrationScaffoldEvidence {
	return integrationScaffoldEvidence{
		DetectFiles:    integrationScaffoldDetectFiles(projectDir),
		BackupExcludes: integrationScaffoldGitignoreExcludes(projectDir),
	}
}

func integrationScaffoldDetectFiles(projectDir string) []string {
	type candidate struct {
		Name     string
		Priority int
	}
	seen := make(map[string]struct{})
	var candidates []candidate

	filepath.WalkDir(projectDir, func(path string, entry os.DirEntry, err error) error { //nolint:errcheck // best-effort evidence collection
		if err != nil {
			return nil
		}
		if path == projectDir {
			return nil
		}
		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if entry.IsDir() {
			if depth > 3 {
				return filepath.SkipDir
			}
			if _, skip := integrationDetectIgnoredDirs[entry.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > 3 {
			return nil
		}
		name := entry.Name()
		priority, ok := integrationScaffoldDetectPriority(name)
		if !ok {
			return nil
		}
		if _, dup := seen[name]; dup {
			return nil
		}
		seen[name] = struct{}{}
		candidates = append(candidates, candidate{Name: name, Priority: priority})
		return nil
	})

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].Name < candidates[j].Name
	})

	files := make([]string, 0, min(len(candidates), integrationScaffoldMaxEvidence))
	for _, candidate := range candidates {
		if len(files) == integrationScaffoldMaxEvidence {
			break
		}
		files = append(files, candidate.Name)
	}
	return files
}

func integrationScaffoldDetectPriority(name string) (int, bool) {
	if strings.ContainsAny(name, "/\\\x00") {
		return 0, false
	}
	lower := strings.ToLower(name)
	if integrationScaffoldSkipDetectName(lower) || integrationScaffoldSensitiveName(lower) {
		return 0, false
	}
	if strings.Contains(lower, "lock") {
		return 0, true
	}
	if strings.Contains(lower, "package") || strings.Contains(lower, "project") ||
		strings.Contains(lower, "manifest") || strings.Contains(lower, "config") ||
		strings.HasSuffix(lower, "file") {
		return 1, true
	}
	switch filepath.Ext(lower) {
	case ".json", ".toml", ".yaml", ".yml", ".mod", ".sum", ".gradle", ".kts",
		".exs", ".cabal", ".tf", ".tla", ".cfg", ".swift":
		return 2, true
	default:
		return 0, false
	}
}

func integrationScaffoldSkipDetectName(lower string) bool {
	switch lower {
	case ".ds_store", ".gitignore", ".gitmodules", ".dockerignore", "readme", "readme.md",
		"license", "license.md", "copying", "changelog", "changelog.md", "contributing.md",
		"security.md", "agents.md":
		return true
	default:
		return false
	}
}

func integrationScaffoldSensitiveName(lower string) bool {
	return strings.HasPrefix(lower, ".env") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "credential") ||
		strings.Contains(lower, "token")
}

func integrationScaffoldGitignoreExcludes(projectDir string) []string {
	data, err := os.ReadFile(filepath.Join(projectDir, ".gitignore"))
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var excludes []string
	for _, line := range strings.Split(string(data), "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") || strings.HasPrefix(pattern, "!") {
			continue
		}
		pattern = strings.TrimPrefix(pattern, "/")
		if pattern == "" || strings.Contains(pattern, "\x00") ||
			strings.HasPrefix(pattern, "../") || integrationScaffoldSensitiveName(strings.ToLower(pattern)) {
			continue
		}
		if _, dup := seen[pattern]; dup {
			continue
		}
		seen[pattern] = struct{}{}
		excludes = append(excludes, pattern)
		if len(excludes) == integrationScaffoldMaxEvidence {
			break
		}
	}
	return excludes
}
