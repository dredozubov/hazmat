package setup

import (
	"fmt"
	"os"
)

type LocalRepoConfig struct {
	RepositoryPath  string
	KeepLatest      int
	KeepDaily       int
	KeepWeekly      int
	CloudEndpoint   string
	CloudBucket     string
	CloudConfigured bool
}

type LocalRepoEnv struct {
	ConfigFilePath    string
	LocalConfigFile   string
	LocalRepoDir      string
	DryRun            bool
	YesAll            bool
	LoadConfig        func() LocalRepoConfig
	SaveConfig        func() error
	InitLocalRepo     func() error
	PrintConfig       func(LocalRepoConfig)
	PreviewCreateRepo func(string)
	OfferCloudSetup   func()
}

func SetupLocalRepo(env LocalRepoEnv, ui StepStatusUI) error {
	ui.Step("Configure snapshot backup")

	cfg := env.loadConfig()
	if _, err := os.Stat(env.ConfigFilePath); os.IsNotExist(err) {
		if !env.DryRun {
			if err := env.saveConfig(); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
		}
	}

	if _, err := os.Stat(env.LocalConfigFile); err == nil {
		ui.SkipDone(fmt.Sprintf("Snapshot repository already configured at %s", env.LocalRepoDir))
	} else if env.DryRun {
		env.previewCreateRepo(env.LocalRepoDir)
	} else {
		if err := env.initLocalRepo(); err != nil {
			return fmt.Errorf("initialize snapshot repo: %w", err)
		}
		ui.Ok(fmt.Sprintf("Snapshot repository created at %s", env.LocalRepoDir))
	}

	env.printConfig(cfg)

	if !cfg.CloudConfigured && !env.DryRun && !env.YesAll {
		env.offerCloudSetup()
	}

	return nil
}

func RollbackLocalRepo(env LocalRepoEnv, ui StepStatusUI) {
	ui.Step("Remove local snapshot repository")

	if _, err := os.Stat(env.LocalRepoDir); os.IsNotExist(err) {
		ui.SkipDone("Local snapshot repository not present")
		return
	}

	os.Remove(env.LocalConfigFile) //nolint:errcheck // best-effort config cleanup during rollback
	if err := os.RemoveAll(env.LocalRepoDir); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", env.LocalRepoDir, err))
	} else {
		ui.Ok(fmt.Sprintf("Removed %s", env.LocalRepoDir))
	}
}

func (env LocalRepoEnv) loadConfig() LocalRepoConfig {
	if env.LoadConfig == nil {
		return LocalRepoConfig{}
	}
	return env.LoadConfig()
}

func (env LocalRepoEnv) saveConfig() error {
	if env.SaveConfig == nil {
		return fmt.Errorf("local repo config save callback is not configured")
	}
	return env.SaveConfig()
}

func (env LocalRepoEnv) initLocalRepo() error {
	if env.InitLocalRepo == nil {
		return fmt.Errorf("local repo init callback is not configured")
	}
	return env.InitLocalRepo()
}

func (env LocalRepoEnv) printConfig(cfg LocalRepoConfig) {
	if env.PrintConfig != nil {
		env.PrintConfig(cfg)
	}
}

func (env LocalRepoEnv) previewCreateRepo(path string) {
	if env.PreviewCreateRepo != nil {
		env.PreviewCreateRepo(path)
	}
}

func (env LocalRepoEnv) offerCloudSetup() {
	if env.OfferCloudSetup != nil {
		env.OfferCloudSetup()
	}
}
