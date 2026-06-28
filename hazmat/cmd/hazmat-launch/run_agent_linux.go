//go:build linux && (amd64 || arm64)

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	linuxspec "hazmat/containment/linux"
	linuxruntime "hazmat/internal/runtime/linux"
	platformlinux "hazmat/platform/linux"
)

type runAgentRequest struct {
	SpecPath     string
	SpecSHA256   string
	SpecNonce    string
	MetadataPath string
}

func runAgentCommand(args []string, _ *launchProfile) {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Fprintln(os.Stdout, "usage: hazmat-launch run-agent --spec <path> --spec-sha256 <hex> --nonce <hex> --metadata <path>")
		return
	}
	request, err := parseRunAgentArgs(args)
	if err != nil {
		die("hazmat-launch: %v", err)
	}
	spec, err := readRunAgentSpec(request)
	if err != nil {
		die("hazmat-launch: %v", err)
	}
	result, err := linuxruntime.RunAgentUserRootHelper(context.Background(), spec, platformlinux.InspectHost(), linuxruntime.RunOptions{
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Sidecar: linuxruntime.SidecarStore{Dir: filepath.Dir(request.MetadataPath)},
	})
	if err != nil {
		die("hazmat-launch: run-agent: %v", err)
	}
	os.Exit(result.ExitCode)
}

func parseRunAgentArgs(args []string) (runAgentRequest, error) {
	var request runAgentRequest
	for len(args) > 0 {
		if len(args) < 2 {
			return runAgentRequest{}, fmt.Errorf("run-agent %s requires a value", args[0])
		}
		value := args[1]
		switch args[0] {
		case "--spec":
			request.SpecPath = value
		case "--spec-sha256":
			request.SpecSHA256 = value
		case "--nonce":
			request.SpecNonce = value
		case "--metadata":
			request.MetadataPath = value
		default:
			return runAgentRequest{}, fmt.Errorf("unknown run-agent argument %q", args[0])
		}
		args = args[2:]
	}
	for name, value := range map[string]string{
		"--spec":        request.SpecPath,
		"--spec-sha256": request.SpecSHA256,
		"--nonce":       request.SpecNonce,
		"--metadata":    request.MetadataPath,
	} {
		if strings.TrimSpace(value) == "" {
			return runAgentRequest{}, fmt.Errorf("run-agent missing %s", name)
		}
	}
	if err := validateRunAgentPath("--spec", request.SpecPath); err != nil {
		return runAgentRequest{}, err
	}
	if err := validateRunAgentPath("--metadata", request.MetadataPath); err != nil {
		return runAgentRequest{}, err
	}
	if filepath.Base(request.MetadataPath) != "metadata.json" {
		return runAgentRequest{}, fmt.Errorf("run-agent --metadata must name metadata.json")
	}
	if _, err := hex.DecodeString(request.SpecSHA256); err != nil || len(request.SpecSHA256) != sha256.Size*2 {
		return runAgentRequest{}, fmt.Errorf("run-agent --spec-sha256 must be a %d-byte hex digest", sha256.Size)
	}
	if raw, err := hex.DecodeString(request.SpecNonce); err != nil || len(raw) != 16 {
		return runAgentRequest{}, fmt.Errorf("run-agent --nonce must be 16 bytes of hex")
	}
	return request, nil
}

func validateRunAgentPath(name, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("run-agent %s path %q must be absolute and clean", name, path)
	}
	return nil
}

func readRunAgentSpec(request runAgentRequest) (linuxspec.LaunchSpec, error) {
	info, err := os.Stat(request.SpecPath)
	if err != nil {
		return linuxspec.LaunchSpec{}, fmt.Errorf("stat run-agent spec: %w", err)
	}
	if !info.Mode().IsRegular() {
		return linuxspec.LaunchSpec{}, fmt.Errorf("run-agent spec must be a regular file")
	}
	data, err := readFileCloexec(request.SpecPath)
	if err != nil {
		return linuxspec.LaunchSpec{}, fmt.Errorf("read run-agent spec: %w", err)
	}
	sum := sha256.Sum256(data)
	if got := fmt.Sprintf("%x", sum[:]); got != request.SpecSHA256 {
		return linuxspec.LaunchSpec{}, fmt.Errorf("run-agent spec digest mismatch")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var spec linuxspec.LaunchSpec
	if err := dec.Decode(&spec); err != nil {
		return linuxspec.LaunchSpec{}, fmt.Errorf("parse run-agent spec: %w", err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return linuxspec.LaunchSpec{}, fmt.Errorf("parse run-agent spec: trailing data")
	}
	return spec, nil
}
