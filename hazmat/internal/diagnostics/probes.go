package diagnostics

import (
	"fmt"

	"hazmat/internal/hostexec"
)

type agentQuietRunner func(hostexec.Env, ...string) error
type agentShellRunner func(hostexec.Env, string) error

// AgentTCPConnect tests whether the agent user can reach host:port. It invokes
// Hazmat's hidden _connect entrypoint through the helper-backed agent
// maintenance path, so the actual TCP dial runs under the agent UID.
func AgentTCPConnect(env hostexec.Env, selfPath, host, port string) bool {
	return agentTCPConnect(env, selfPath, host, port, hostexec.AsAgentQuiet, hostexec.AsAgentShellQuiet)
}

func agentTCPConnect(env hostexec.Env, selfPath, host, port string, runQuiet agentQuietRunner, runShell agentShellRunner) bool {
	if selfPath != "" {
		return runQuiet(env, selfPath, "_connect", host, port) == nil
	}
	script := fmt.Sprintf(
		"timeout 3 bash -c 'echo > /dev/tcp/%s/%s' 2>/dev/null",
		host, port,
	)
	return runShell(env, script) == nil
}
