package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	gitHTTPSCredentialSocketEnv = "HAZMAT_GIT_HTTPS_CREDENTIAL_SOCKET"
)

var (
	gitHTTPSAgentCredentialsPath = agentHome + "/.config/git/credentials"
	gitHTTPSAgentGitConfigPath   = agentHome + "/.gitconfig"
	gitHTTPSExecutablePath       = os.Executable
	gitHTTPSWriteRuntimeFile     = writeGitSSHRuntimeFile
	gitHTTPSRunCredentialStore   = runGitHTTPSCredentialStore
)

type gitHTTPSCredentialRequest struct {
	Operation string `json:"operation"`
	Payload   []byte `json:"payload"`
}

type gitHTTPSCredentialResponse struct {
	Stdout []byte `json:"stdout,omitempty"`
	Stderr []byte `json:"stderr,omitempty"`
	Error  string `json:"error,omitempty"`
}

type gitHTTPSCredentialService struct {
	storePath  string
	runtimeDir string
	socketPath string
	listener   net.Listener
	done       chan struct{}
	closeOnce  sync.Once
}

func gitHTTPSCredentialStorePathForHome(home string) string {
	return mustCredentialStorePathForHome(home, credentialGitHTTPSAgentStore)
}

func prepareGitHTTPSCredentialRuntime() (preparedSessionRuntime, error) {
	runtime := preparedSessionRuntime{Cleanup: func() {}}

	home, err := os.UserHomeDir()
	if err != nil {
		return runtime, fmt.Errorf("determine home directory for Git HTTPS credentials: %w", err)
	}
	storePath := gitHTTPSCredentialStorePathForHome(home)
	if _, err := migrateLegacyGitHTTPSCredentials(storePath); err != nil {
		return runtime, err
	}

	runtimeDir := filepath.Join(seatbeltProfileDir, "git-https", fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := agentEnsureSharedDir(runtimeDir, 0o2770); err != nil {
		return runtime, fmt.Errorf("prepare Git HTTPS credential runtime dir: %w", err)
	}

	service, err := startGitHTTPSCredentialService(storePath, runtimeDir)
	if err != nil {
		_ = os.RemoveAll(runtimeDir)
		return runtime, err
	}

	helperPath, err := gitHTTPSExecutablePath()
	if err != nil {
		service.Close()
		return preparedSessionRuntime{Cleanup: func() {}}, fmt.Errorf("resolve hazmat binary for Git HTTPS credential helper: %w", err)
	}

	wrapperPath := filepath.Join(runtimeDir, "git-credential-hazmat")
	wrapperScript := buildGitHTTPSCredentialHelperScript(helperPath, service.socketPath)
	if err := gitHTTPSWriteRuntimeFile(wrapperPath, []byte(wrapperScript), 0o750); err != nil {
		service.Close()
		return preparedSessionRuntime{Cleanup: func() {}}, fmt.Errorf("write Git HTTPS credential helper: %w", err)
	}

	runtime.Cleanup = service.Close
	runtime.EnvPairs = []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=credential.helper",
		"GIT_CONFIG_VALUE_1=" + wrapperPath,
		gitHTTPSCredentialSocketEnv + "=" + service.socketPath,
	}
	return runtime, nil
}

func startGitHTTPSCredentialService(storePath, runtimeDir string) (*gitHTTPSCredentialService, error) {
	socketPath := filepath.Join(runtimeDir, "credential.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("start Git HTTPS credential listener: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("set Git HTTPS credential socket mode: %w", err)
	}

	service := &gitHTTPSCredentialService{
		storePath:  storePath,
		runtimeDir: runtimeDir,
		socketPath: socketPath,
		listener:   listener,
		done:       make(chan struct{}),
	}
	go service.serve()
	return service, nil
}

func (s *gitHTTPSCredentialService) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.listener != nil {
			_ = s.listener.Close()
		}
		if s.done != nil {
			<-s.done
		}
		_ = os.RemoveAll(s.runtimeDir)
	})
}

func (s *gitHTTPSCredentialService) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		s.handleConn(conn)
	}
}

func (s *gitHTTPSCredentialService) handleConn(conn net.Conn) {
	defer conn.Close()

	var req gitHTTPSCredentialRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(gitHTTPSCredentialResponse{
			Error: fmt.Sprintf("decode Git HTTPS credential request: %v", err),
		})
		return
	}

	stdout, stderr, err := gitHTTPSRunCredentialStore(s.storePath, req.Operation, req.Payload)
	resp := gitHTTPSCredentialResponse{Stdout: stdout, Stderr: stderr}
	if err != nil {
		resp.Error = err.Error()
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func requestGitHTTPSCredential(socketPath, operation string, payload []byte) (gitHTTPSCredentialResponse, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return gitHTTPSCredentialResponse{}, fmt.Errorf("connect Git HTTPS credential service: %w", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(gitHTTPSCredentialRequest{
		Operation: operation,
		Payload:   payload,
	}); err != nil {
		return gitHTTPSCredentialResponse{}, fmt.Errorf("request Git HTTPS credential operation: %w", err)
	}

	var resp gitHTTPSCredentialResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return gitHTTPSCredentialResponse{}, fmt.Errorf("read Git HTTPS credential response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

func runGitHTTPSCredentialStore(storePath, operation string, payload []byte) ([]byte, []byte, error) {
	switch operation {
	case "get", "store", "erase":
	default:
		return nil, nil, fmt.Errorf("unsupported Git HTTPS credential operation %q", operation)
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create Git HTTPS credential store directory: %w", err)
	}
	cmd, err := hostGitCommand("credential-store", "--file", storePath, operation)
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("git credential-store %s: %s", operation, msg)
	}
	if operation != "get" {
		if info, err := os.Stat(storePath); err == nil && !info.IsDir() {
			if err := os.Chmod(storePath, 0o600); err != nil {
				return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("set Git HTTPS credential store mode: %w", err)
			}
		}
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func migrateLegacyGitHTTPSCredentials(storePath string) (bool, error) {
	legacy, legacyExists, err := readAgentSecretFile(gitHTTPSAgentCredentialsPath)
	if err != nil || !legacyExists {
		return false, err
	}
	legacy = bytes.TrimSpace(legacy)
	if len(legacy) == 0 {
		return true, removeAgentSecretFile(gitHTTPSAgentCredentialsPath)
	}

	hostRaw, hostExists, err := readHostStoredSecretFile(storePath)
	if err != nil {
		return false, err
	}
	merged := mergeGitCredentialStoreLines(hostRaw, legacy)
	if len(merged) > 0 && (!hostExists || !bytes.Equal(bytes.TrimSpace(hostRaw), bytes.TrimSpace(merged))) {
		if err := writeHostStoredSecretFile(storePath, append(merged, '\n')); err != nil {
			return false, err
		}
	}
	if err := removeAgentSecretFile(gitHTTPSAgentCredentialsPath); err != nil {
		return false, err
	}
	return true, nil
}

func mergeGitCredentialStoreLines(existing, legacy []byte) []byte {
	seen := map[string]struct{}{}
	var lines []string
	for _, raw := range [][]byte{existing, legacy} {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n"))
}

func buildGitHTTPSCredentialHelperScript(helperPath, socketPath string) string {
	quoted := shellQuote([]string{helperPath, socketPath})
	return strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"helper=" + quoted[0],
		"socket=${" + gitHTTPSCredentialSocketEnv + ":-" + quoted[1] + "}",
		"operation=${1:-get}",
		"exec \"$helper\" _git_https_credential \"$socket\" \"$operation\"",
		"",
	}, "\n")
}
