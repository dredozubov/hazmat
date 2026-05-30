//go:build hazmat_debug && !darwin && !linux

package main

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
	return fmt.Errorf("hazmat trace %s is currently implemented for macOS/Darwin only", harness)
}

func (unsupportedTraceBackend) observerDescription() string {
	return "platform observers"
}

func (unsupportedTraceBackend) syscallFlagHelp() string {
	return "Attempt host-side syscall/filesystem probes"
}

func (unsupportedTraceBackend) preflight(traceHarnessSpec, traceOptions) error {
	return fmt.Errorf("hazmat trace is not implemented on this platform")
}

func (unsupportedTraceBackend) writeToolProbe(string, traceHarnessSpec) {}

func (unsupportedTraceBackend) writeHostSnapshot(string, traceHarnessSpec, string) {}

func (unsupportedTraceBackend) startObservers(context.Context, string, traceHarnessSpec, traceOptions) (traceObserverSet, error) {
	return noopTraceObservers{}, nil
}

func (unsupportedTraceBackend) runLaunch(dir string, opts traceOptions, launchArgs []string) error {
	return runTraceLaunch(dir, opts, launchArgs)
}

func traceScriptCommandArgs(transcript, self string, launchArgs []string) []string {
	return append([]string{"-q", transcript, self}, launchArgs...)
}

func (unsupportedTraceBackend) writePostLaunchLogs(string, traceHarnessSpec, time.Time, time.Time) {}

func (unsupportedTraceBackend) indicatorFiles() []string {
	return nil
}
