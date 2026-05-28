package main

import (
	"runtime"

	"hazmat/sessionbackend"
)

type sessionBackendPlan = sessionbackend.Plan

func buildSessionBackendPlan(cfg sessionConfig, mode sessionMode) sessionBackendPlan {
	return buildSessionBackendPlanForGOOS(cfg, mode, runtime.GOOS)
}

func buildSessionBackendPlanForGOOS(cfg sessionConfig, mode sessionMode, goos string) sessionBackendPlan {
	return sessionbackend.BuildPlan(sessionbackend.Input{
		Target:             cfg.Target,
		Mode:               mode,
		ProjectDir:         cfg.ProjectDir,
		ReadOnlyDirs:       cfg.ReadDirs,
		ReadWriteDirs:      cfg.WriteDirs,
		NetworkMode:        cfg.NetworkMode,
		Integrations:       cfg.ActiveIntegrations,
		IntegrationEnvKeys: integrationEnvKeys(cfg.IntegrationEnv),
		GOOS:               goos,
	})
}
