package hazmat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"hazmat/internal/agententry"
	"hazmat/internal/runtime/launchbroker"
)

func runLaunchBrokerAgentEntry(ctx context.Context, req agententry.LaunchBrokerRequest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	service, err := launchbroker.StartService(ctx, launchBrokerAgentEntryServiceConfig(req))
	if err != nil {
		return fmt.Errorf("start launch broker service: %w", err)
	}
	defer service.Close() //nolint:errcheck // service result is returned through Done below.

	select {
	case <-ctx.Done():
		if err := service.Close(); err != nil {
			return err
		}
		return nil
	case err := <-service.Done():
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return err
	}
}

func launchBrokerAgentEntryServiceConfig(req agententry.LaunchBrokerRequest) launchbroker.ServiceConfig {
	return launchbroker.ServiceConfig{
		SocketPath:      req.SocketPath,
		SocketMode:      0o660,
		ExpectedPeerUID: req.ExpectedPeerUID,
		Helper: launchbroker.HelperExecutorConfig{
			LaunchHelperPath: req.LaunchHelper,
			Profile:          sessionPreparationProfileEnabled(),
		},
	}
}
