package main

import (
	"io"

	"hazmat/sessioncontract"
	"hazmat/sessionmeta"
)

type sessionNetworkMode = sessionmeta.NetworkMode

const (
	sessionNetworkDefault = sessionmeta.NetworkDefault
	sessionNetworkNone    = sessionmeta.NetworkNone
)

type sessionLaunchMetadata = sessionmeta.LaunchMetadata
type sessionNetworkPolicyMetadata = sessionmeta.NetworkPolicyMetadata

func parseSessionNetworkMode(raw string) (sessionNetworkMode, error) {
	return sessionmeta.ParseNetworkMode(raw)
}

func normalizeSessionNetworkMode(mode sessionNetworkMode) sessionNetworkMode {
	return sessionmeta.NormalizeNetworkMode(mode)
}

func sessionNetworkContractLabel(cfg sessionConfig, mode sessionMode) string {
	return sessionmeta.NetworkContractLabel(cfg.NetworkMode, mode)
}

func buildSessionLaunchMetadata(cfg sessionConfig, mode sessionMode) sessionLaunchMetadata {
	return sessionmeta.BuildLaunchMetadata(sessionLaunchMetadataInput(cfg, mode))
}

func emitSessionLaunchMetadataJSON(w io.Writer, cfg sessionConfig, mode sessionMode) error {
	return sessionmeta.EmitLaunchMetadataJSON(w, sessionLaunchMetadataInput(cfg, mode))
}

func marshalSessionLaunchMetadataJSON(cfg sessionConfig, mode sessionMode) (string, error) {
	return sessionmeta.MarshalLaunchMetadataJSON(sessionLaunchMetadataInput(cfg, mode))
}

func sessionLaunchMetadataInput(cfg sessionConfig, mode sessionMode) sessionmeta.LaunchMetadataInput {
	return sessionContractRequest(cfg).LaunchMetadataInput(mode)
}

func sessionContractRequest(cfg sessionConfig) sessioncontract.Request {
	return sessioncontract.Request{
		Target:              cfg.Target,
		ProjectDir:          cfg.ProjectDir,
		ReadOnlyDirs:        cfg.ReadDirs,
		AutoReadOnlyDirs:    cfg.AutoReadDirs,
		UserReadOnlyDirs:    cfg.UserReadDirs,
		ReadWriteExtensions: cfg.WriteDirs,
		NetworkMode:         cfg.NetworkMode,
		Integrations:        cfg.ActiveIntegrations,
		HarnessID:           string(cfg.HarnessID),
		MetadataJSON:        cfg.EmitSessionMetadataJSON,
	}
}
