package hazmat

import (
	"context"
	"fmt"
	"strings"

	"hazmat/sessionlaunch"
)

type sessionLauncher struct{}

// NewSessionLauncher returns Hazmat's protocol-neutral session preparation
// facade.
func NewSessionLauncher() sessionlaunch.Launcher {
	return sessionLauncher{}
}

func (sessionLauncher) Prepare(ctx context.Context, request sessionlaunch.LaunchRequest) (sessionlaunch.PreparedSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return sessionlaunch.PreparedSession{}, err
	}

	request = request.Normalized()
	if strings.TrimSpace(request.Target) == "" {
		return sessionlaunch.PreparedSession{}, fmt.Errorf("sessionlaunch target is required")
	}

	prepared, err := prepareLaunchSessionWithProgress(
		request.Target,
		harnessSessionOptsForLaunchRequest(request),
		request.Options.SupportsSandbox,
		nil,
		request.Options.InteractiveRepoSetup,
		request.Options.PersistRepoSetup,
	)
	if err != nil {
		return sessionlaunch.PreparedSession{}, err
	}
	if err := ctx.Err(); err != nil {
		return sessionlaunch.PreparedSession{}, err
	}

	request = preparedLaunchRequest(request, prepared)
	plan := buildSessionPlanForHostFacts(prepared.Config.Target, prepared.Config, prepared.Mode, request.Options.SkipSnapshot, currentHostFacts())
	return sessionlaunch.NewPreparedSession(sessionlaunch.PreparedSessionInput{
		Request:     request,
		Plan:        plan,
		BackendPlan: prepared.BackendPlan,
		Mode:        prepared.Mode,
		RuntimeDir:  prepared.Config.TempDir,
	}), nil
}

func preparedLaunchRequest(request sessionlaunch.LaunchRequest, prepared preparedSession) sessionlaunch.LaunchRequest {
	request.Target = prepared.Config.Target
	request.ProjectDir = prepared.Config.ProjectDir
	request.ReadOnly = append([]string(nil), prepared.Config.ReadDirs...)
	request.ReadWrite = append([]string(nil), prepared.Config.WriteDirs...)
	request.Integrations = append([]string(nil), prepared.Config.ActiveIntegrations...)
	request.NetworkMode = prepared.Config.NetworkMode
	return request
}

func harnessSessionOptsForLaunchRequest(request sessionlaunch.LaunchRequest) harnessSessionOpts {
	return harnessSessionOpts{
		project:                      request.ProjectDir,
		readDirs:                     request.ReadOnlyDirs(),
		writeDirs:                    request.ReadWriteDirs(),
		integrations:                 request.IntegrationNames(),
		skipAutoIntegrations:         request.Options.SkipAutoIntegrations,
		skipIntegrationHints:         request.Options.SkipIntegrationHints,
		skipRepoSetupDiscovery:       request.Options.SkipRepoSetupDiscovery,
		skipGitSafeDirectoryPlanning: request.Options.SkipGitSafeDirectoryPlanning,
		skipAmbientAccessGrants:      request.Options.SkipAmbientAccessGrants,
		skipGitHTTPSRuntime:          request.Options.SkipGitHTTPSRuntime,
		skipGoModCacheEnv:            request.Options.SkipGoModCacheEnv,
		skipProjectHooks:             request.Options.SkipProjectHooks,
		skipDockerDetection:          request.Options.SkipDockerDetection,
		skipHarnessAssetsSync:        request.Options.SkipHarnessAssetsSync,
		noBackup:                     request.Options.SkipSnapshot,
		github:                       request.Options.GitHub,
		useSandbox:                   request.Options.UseSandbox,
		allowDocker:                  request.Options.AllowDocker,
		dockerMode:                   request.Options.DockerMode,
		dockerModeExplicit:           request.Options.DockerModeExplicit,
		networkMode:                  string(request.NetworkMode),
		networkModeExplicit:          request.Options.NetworkModeExplicit,
		metadataJSON:                 request.Options.MetadataJSON,
		auditInstall:                 request.Options.AuditInstall,
		planOnly:                     request.Options.PlanOnly,
		runtimeProvider:              request.Options.RuntimeProvider,
		runtimeProviderExplicit:      request.Options.RuntimeProviderExplicit,
	}
}
