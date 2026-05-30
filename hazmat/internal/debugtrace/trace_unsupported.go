//go:build hazmat_debug && !darwin && !linux

package debugtrace

import (
	"context"
	"fmt"
	"time"
)

type unsupportedTraceBackend struct{}

func currentTraceBackend() traceBackend {
	return unsupportedTraceBackend{}
}

func (unsupportedTraceBackend) name() string {
	return "unsupported"
}

func (unsupportedTraceBackend) supported() bool {
	return false
}

func (unsupportedTraceBackend) unsupportedError(harness HarnessID) error {
	return fmt.Errorf("hazmat trace %s is currently implemented for macOS/Darwin and Linux only", harness)
}

func (unsupportedTraceBackend) observerDescription() string {
	return "platform observers"
}

func (unsupportedTraceBackend) syscallFlagHelp() string {
	return "Attempt host-side syscall/filesystem probes"
}

func (unsupportedTraceBackend) preflight(Env, HarnessSpec, Options) error {
	return fmt.Errorf("hazmat trace is not implemented on this platform")
}

func (unsupportedTraceBackend) writeToolProbe(Env, string, HarnessSpec) error {
	return nil
}

func (unsupportedTraceBackend) writeHostSnapshot(Env, string, HarnessSpec, string) error {
	return nil
}

func (unsupportedTraceBackend) startObservers(context.Context, Env, string, HarnessSpec, Options) (traceObserverSet, error) {
	return noopTraceObservers{}, nil
}

func (unsupportedTraceBackend) runLaunch(env Env, dir string, opts Options, launchArgs []string) error {
	return runTraceLaunch(env, dir, opts, launchArgs)
}

func traceScriptCommandArgs(transcript, self string, launchArgs []string) []string {
	return append([]string{"-q", transcript, self}, launchArgs...)
}

func (unsupportedTraceBackend) writePostLaunchLogs(Env, string, HarnessSpec, time.Time, time.Time) error {
	return nil
}

func (unsupportedTraceBackend) indicatorFiles() []string {
	return nil
}
