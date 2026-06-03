package backupruntime

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type SnapshotFunc func(projectDir, command string, ignoreRules ...string) error

type PreSessionSnapshotOptions struct {
	ProjectDir     string
	Command        string
	BackupExcludes []string
	Skip           bool
	Snapshot       SnapshotFunc
	Stderr         io.Writer
	Now            func() time.Time
}

func PreSessionSnapshot(opts PreSessionSnapshotOptions) {
	if opts.Skip {
		return
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	start := now()
	fmt.Fprintf(stderr, "  Snapshot: %s ... ", opts.ProjectDir)
	snapshot := opts.Snapshot
	if snapshot == nil {
		snapshot = func(string, string, ...string) error {
			return errors.New("snapshot function is not configured")
		}
	}
	if err := snapshot(opts.ProjectDir, opts.Command, opts.BackupExcludes...); err != nil {
		fmt.Fprintf(stderr, "\n  Warning: pre-session snapshot failed: %v\n", err)
		fmt.Fprintln(stderr, "  Session will proceed without a restore point.")
		return
	}
	fmt.Fprintf(stderr, "done (%.1fs)\n", now().Sub(start).Seconds())
}
