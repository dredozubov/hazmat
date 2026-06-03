package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupLocalRepoCreatesConfigAndRepo(t *testing.T) {
	tmp := t.TempDir()
	var saved bool
	var initialized bool
	var printed bool

	env := LocalRepoEnv{
		ConfigFilePath:  filepath.Join(tmp, "config.yaml"),
		LocalConfigFile: filepath.Join(tmp, "repo.config"),
		LocalRepoDir:    filepath.Join(tmp, "repo"),
		LoadConfig: func() LocalRepoConfig {
			return LocalRepoConfig{RepositoryPath: filepath.Join(tmp, "repo")}
		},
		SaveConfig: func() error {
			saved = true
			return nil
		},
		InitLocalRepo: func() error {
			initialized = true
			return nil
		},
		PrintConfig: func(LocalRepoConfig) {
			printed = true
		},
	}

	if err := SetupLocalRepo(env, &fakeToolingUI{}); err != nil {
		t.Fatalf("SetupLocalRepo: %v", err)
	}
	if !saved || !initialized || !printed {
		t.Fatalf("callbacks saved=%v initialized=%v printed=%v, want all true", saved, initialized, printed)
	}
}

func TestSetupLocalRepoDryRunPreviewsWithoutSavingOrInit(t *testing.T) {
	tmp := t.TempDir()
	var previewPath string

	env := LocalRepoEnv{
		ConfigFilePath:  filepath.Join(tmp, "config.yaml"),
		LocalConfigFile: filepath.Join(tmp, "repo.config"),
		LocalRepoDir:    filepath.Join(tmp, "repo"),
		DryRun:          true,
		SaveConfig: func() error {
			t.Fatal("SaveConfig should not run during dry-run")
			return nil
		},
		InitLocalRepo: func() error {
			t.Fatal("InitLocalRepo should not run during dry-run")
			return nil
		},
		PreviewCreateRepo: func(path string) {
			previewPath = path
		},
	}

	if err := SetupLocalRepo(env, &fakeToolingUI{}); err != nil {
		t.Fatalf("SetupLocalRepo: %v", err)
	}
	if previewPath != env.LocalRepoDir {
		t.Fatalf("preview path = %q, want %q", previewPath, env.LocalRepoDir)
	}
}

func TestSetupLocalRepoOffersCloudOnlyWhenEligible(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte("configured\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "repo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "repo.config"), []byte("repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var offered bool
	env := LocalRepoEnv{
		ConfigFilePath:  filepath.Join(tmp, "config.yaml"),
		LocalConfigFile: filepath.Join(tmp, "repo.config"),
		LocalRepoDir:    filepath.Join(tmp, "repo"),
		LoadConfig: func() LocalRepoConfig {
			return LocalRepoConfig{}
		},
		OfferCloudSetup: func() {
			offered = true
		},
	}

	if err := SetupLocalRepo(env, &fakeToolingUI{}); err != nil {
		t.Fatalf("SetupLocalRepo: %v", err)
	}
	if !offered {
		t.Fatal("cloud setup was not offered for unconfigured non-dry-run local repo")
	}

	offered = false
	env.YesAll = true
	if err := SetupLocalRepo(env, &fakeToolingUI{}); err != nil {
		t.Fatalf("SetupLocalRepo with YesAll: %v", err)
	}
	if offered {
		t.Fatal("cloud setup offered even though YesAll was set")
	}
}

func TestRollbackLocalRepoRemovesRepoAndConfig(t *testing.T) {
	tmp := t.TempDir()
	env := LocalRepoEnv{
		LocalConfigFile: filepath.Join(tmp, "repo.config"),
		LocalRepoDir:    filepath.Join(tmp, "repo"),
	}
	if err := os.MkdirAll(env.LocalRepoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.LocalConfigFile, []byte("repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	RollbackLocalRepo(env, &fakeToolingUI{})

	if _, err := os.Stat(env.LocalRepoDir); !os.IsNotExist(err) {
		t.Fatalf("repo dir still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(env.LocalConfigFile); !os.IsNotExist(err) {
		t.Fatalf("repo config still exists or stat failed: %v", err)
	}
}
