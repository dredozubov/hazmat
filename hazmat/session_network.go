package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type sessionNetworkMode string

const (
	sessionNetworkDefault sessionNetworkMode = "default"
	sessionNetworkNone    sessionNetworkMode = "none"
)

const sessionLaunchMetadataFormatVersion = 1

type sessionLaunchMetadata struct {
	FormatVersion int                          `json:"format_version"`
	Kind          string                       `json:"kind"`
	Target        string                       `json:"target,omitempty"`
	Mode          string                       `json:"mode"`
	ModeLabel     string                       `json:"mode_label"`
	ProjectDir    string                       `json:"project_dir"`
	NetworkPolicy sessionNetworkPolicyMetadata `json:"network_policy"`
}

type sessionNetworkPolicyMetadata struct {
	Requested       string   `json:"requested"`
	Effective       string   `json:"effective"`
	Enforced        bool     `json:"enforced"`
	Enforcement     string   `json:"enforcement"`
	DenyAllEgress   bool     `json:"deny_all_egress"`
	Denied          []string `json:"denied,omitempty"`
	CleanupRequired bool     `json:"cleanup_required"`
}

func parseSessionNetworkMode(raw string) (sessionNetworkMode, error) {
	mode := sessionNetworkMode(strings.ToLower(strings.TrimSpace(raw)))
	if mode == "" {
		return sessionNetworkDefault, nil
	}
	switch mode {
	case sessionNetworkDefault, sessionNetworkNone:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid network mode %q (want default or none)", raw)
	}
}

func normalizeSessionNetworkMode(mode sessionNetworkMode) sessionNetworkMode {
	if mode == "" {
		return sessionNetworkDefault
	}
	return mode
}

func (m sessionNetworkMode) String() string {
	return string(normalizeSessionNetworkMode(m))
}

func (m sessionNetworkMode) contractLabel() string {
	switch normalizeSessionNetworkMode(m) {
	case sessionNetworkNone:
		return "none (deny outbound IPv4, IPv6, and DNS)"
	default:
		return "default (outbound allowed)"
	}
}

func sessionNetworkContractLabel(cfg sessionConfig, mode sessionMode) string {
	if mode == sessionModeDockerSandbox {
		return "Docker Sandbox profile (deny by default)"
	}
	return normalizeSessionNetworkMode(cfg.NetworkMode).contractLabel()
}

func buildSessionLaunchMetadata(cfg sessionConfig, mode sessionMode) sessionLaunchMetadata {
	return sessionLaunchMetadata{
		FormatVersion: sessionLaunchMetadataFormatVersion,
		Kind:          "hazmat.session",
		Target:        cfg.Target,
		Mode:          string(mode),
		ModeLabel:     mode.label(),
		ProjectDir:    cfg.ProjectDir,
		NetworkPolicy: buildSessionNetworkPolicyMetadata(cfg, mode),
	}
}

func buildSessionNetworkPolicyMetadata(cfg sessionConfig, mode sessionMode) sessionNetworkPolicyMetadata {
	requested := normalizeSessionNetworkMode(cfg.NetworkMode)
	meta := sessionNetworkPolicyMetadata{
		Requested:       requested.String(),
		Effective:       requested.String(),
		Enforced:        true,
		Enforcement:     "native-seatbelt",
		CleanupRequired: false,
	}
	if requested == sessionNetworkNone {
		meta.DenyAllEgress = true
		meta.Denied = []string{"outbound-ipv4", "outbound-ipv6", "dns"}
	}
	if mode == sessionModeDockerSandbox {
		meta.Effective = "sandbox-profile"
		meta.Enforcement = "docker-sandbox-network-profile"
		meta.CleanupRequired = true
		meta.DenyAllEgress = false
		meta.Denied = nil
	}
	return meta
}

func emitSessionLaunchMetadataJSON(w io.Writer, cfg sessionConfig, mode sessionMode) error {
	metadataJSON, err := marshalSessionLaunchMetadataJSON(cfg, mode)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, metadataJSON)
	return err
}

func marshalSessionLaunchMetadataJSON(cfg sessionConfig, mode sessionMode) (string, error) {
	data, err := json.Marshal(buildSessionLaunchMetadata(cfg, mode))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
