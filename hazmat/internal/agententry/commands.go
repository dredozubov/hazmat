package agententry

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

type GitSSHTransportRunner func(socketPath string, args []string) int

type GitHTTPSCredentialResponse struct {
	Stdout []byte
	Stderr []byte
}

type GitHTTPSCredentialRequester func(socketPath, operation string, payload []byte) (GitHTTPSCredentialResponse, error)

type LaunchBrokerRequest struct {
	SocketPath      string
	ExpectedPeerUID int
	LaunchHelper    string
}

type LaunchBrokerRunner func(context.Context, LaunchBrokerRequest) error

const LaunchBrokerCommandName = "_launch_broker"

// NewConnectCommand returns the hidden command used by diagnostics to probe
// host:port reachability as the agent user.
func NewConnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "_connect <host> <port>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		Run: func(_ *cobra.Command, args []string) {
			conn, err := net.DialTimeout("tcp",
				net.JoinHostPort(args[0], args[1]),
				5*time.Second,
			)
			if err != nil {
				os.Exit(1)
			}
			conn.Close() //nolint:errcheck // diagnostic probe; process exits immediately
		},
	}
}

func NewGitSSHTransportCommand(run GitSSHTransportRunner) *cobra.Command {
	return newGitSSHTransportCommand(run, os.Exit)
}

func newGitSSHTransportCommand(run GitSSHTransportRunner, exit func(int)) *cobra.Command {
	if run == nil {
		run = func(string, []string) int { return 1 }
	}
	if exit == nil {
		exit = os.Exit
	}
	return &cobra.Command{
		Use:                "_git_ssh_transport <socket> [ssh-args...]",
		Hidden:             true,
		DisableFlagParsing: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("_git_ssh_transport requires a broker socket and ssh arguments")
			}
			return nil
		},
		Run: func(_ *cobra.Command, args []string) {
			exit(run(args[0], args[1:]))
		},
	}
}

func NewGitHTTPSCredentialCommand(request GitHTTPSCredentialRequester) *cobra.Command {
	return &cobra.Command{
		Use:    "_git_https_credential <socket> <operation>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read Git HTTPS credential request from stdin: %w", err)
			}
			if request == nil {
				return fmt.Errorf("Git HTTPS credential requester is not configured")
			}
			resp, err := request(args[0], args[1], payload)
			if len(resp.Stdout) > 0 {
				if _, writeErr := cmd.OutOrStdout().Write(resp.Stdout); writeErr != nil && err == nil {
					err = writeErr
				}
			}
			if len(resp.Stderr) > 0 {
				if _, writeErr := cmd.ErrOrStderr().Write(resp.Stderr); writeErr != nil && err == nil {
					err = writeErr
				}
			}
			return err
		},
	}
}

func NewLaunchBrokerCommand(run LaunchBrokerRunner) *cobra.Command {
	return &cobra.Command{
		Use:    LaunchBrokerCommandName + " <socket> <expected-peer-uid> <launch-helper>",
		Hidden: true,
		Args:   cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if run == nil {
				return fmt.Errorf("launch broker runner is not configured")
			}
			uid, err := strconv.Atoi(args[1])
			if err != nil || uid <= 0 {
				return fmt.Errorf("expected peer uid must be a positive integer, got %q", args[1])
			}
			return run(cmd.Context(), LaunchBrokerRequest{
				SocketPath:      args[0],
				ExpectedPeerUID: uid,
				LaunchHelper:    args[2],
			})
		},
	}
}
