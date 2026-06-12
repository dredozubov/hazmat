package hazmat

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseLSOFEgressAuditSnapshot(t *testing.T) {
	raw := `COMMAND   PID  USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
node    12345 agent   27u  IPv4 0xabcd      0t0  TCP 10.0.0.2:52344->104.16.25.34:443 (ESTABLISHED)
curl    12346 agent    5u  IPv6 0xabce      0t0  TCP [fd00::1]:52345->[2606:4700::6810:1922]:443 (ESTABLISHED)
listen  12347 agent    6u  IPv4 0xabcf      0t0  TCP 127.0.0.1:3000 (LISTEN)
`

	snapshot := parseLSOFEgressAuditSnapshot(raw, time.Unix(100, 0))
	if got := len(snapshot.Endpoints); got != 2 {
		t.Fatalf("endpoints = %d, want 2: %#v", got, snapshot.Endpoints)
	}
	for _, want := range []egressAuditEndpoint{
		{Protocol: "tcp", Host: "104.16.25.34", Port: "443"},
		{Protocol: "tcp", Host: "2606:4700::6810:1922", Port: "443"},
	} {
		if _, ok := snapshot.Endpoints[want.key()]; !ok {
			t.Fatalf("snapshot missing endpoint %#v in %#v", want, snapshot.Endpoints)
		}
	}
}

func TestBuildAuditInstallReportClassifiesKnownAndReviewEndpoints(t *testing.T) {
	baseline := newEgressAuditSnapshot(time.Unix(100, 0))
	baseline.add(egressAuditEndpoint{Protocol: "tcp", Host: "github.com", Port: "443"})
	observed := newEgressAuditSnapshot(time.Unix(101, 0))
	observed.add(egressAuditEndpoint{Protocol: "tcp", Host: "github.com", Port: "443"})
	observed.add(egressAuditEndpoint{Protocol: "tcp", Host: "registry.npmjs.org", Port: "443"})
	observed.add(egressAuditEndpoint{Protocol: "tcp", Host: "example.invalid", Port: "443"})

	report := buildAuditInstallReport(baseline, observed, nil)
	if report.BaselineCount != 1 || len(report.NewEndpoints) != 2 {
		t.Fatalf("report counts = baseline %d new %d", report.BaselineCount, len(report.NewEndpoints))
	}
	if len(report.Known) != 1 || report.Known[0].Host != "registry.npmjs.org" {
		t.Fatalf("Known = %#v", report.Known)
	}
	if len(report.Review) != 1 || report.Review[0].Host != "example.invalid" {
		t.Fatalf("Review = %#v", report.Review)
	}
}

func TestRunAuditInstallExecReportsObservedEndpoints(t *testing.T) {
	originalInterval := egressAuditPollInterval
	egressAuditPollInterval = time.Millisecond
	defer func() { egressAuditPollInterval = originalInterval }()

	empty := newEgressAuditSnapshot(time.Unix(100, 0))
	withEndpoint := newEgressAuditSnapshot(time.Unix(101, 0))
	withEndpoint.add(egressAuditEndpoint{Protocol: "tcp", Host: "example.invalid", Port: "443"})
	collector := &sequenceEgressAuditCollector{
		snapshots: []egressAuditSnapshot{empty, withEndpoint, withEndpoint},
	}
	var out bytes.Buffer

	err := runAuditInstallExec(&out, collector, func() error {
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("runAuditInstallExec: %v", err)
	}
	if !strings.Contains(out.String(), "new endpoints observed: 1") ||
		!strings.Contains(out.String(), "tcp example.invalid:443") ||
		!strings.Contains(out.String(), "observational only") {
		t.Fatalf("report output = %q", out.String())
	}
}

func TestRunAuditInstallExecBaselineFailurePreventsCommand(t *testing.T) {
	collector := &sequenceEgressAuditCollector{errors: []error{errors.New("lsof unavailable")}}
	ran := false
	err := runAuditInstallExec(ioDiscard{}, collector, func() error {
		ran = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "baseline egress snapshot") {
		t.Fatalf("error = %v", err)
	}
	if ran {
		t.Fatal("command ran after baseline telemetry failed")
	}
}

func TestResolvePreparedSessionAddsAuditInstallNote(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	prepared, err := resolvePreparedSession("exec", harnessSessionOpts{
		project:      t.TempDir(),
		auditInstall: true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession: %v", err)
	}
	if !sessionNotesContain(prepared.Config.SessionNotes, "Install audit is active") {
		t.Fatalf("SessionNotes = %v", prepared.Config.SessionNotes)
	}
}

func TestResolvePreparedSessionRejectsAuditInstallForDockerSandbox(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	_, err := resolvePreparedSession("exec", harnessSessionOpts{
		project:            t.TempDir(),
		dockerMode:         string(dockerModeSandbox),
		dockerModeExplicit: true,
		auditInstall:       true,
	}, true)
	if err == nil || !strings.Contains(err.Error(), "--audit-install is currently supported only for native") {
		t.Fatalf("error = %v", err)
	}
}

type sequenceEgressAuditCollector struct {
	mu        sync.Mutex
	snapshots []egressAuditSnapshot
	errors    []error
	calls     int
}

func (c *sequenceEgressAuditCollector) Snapshot() (egressAuditSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.calls
	c.calls++
	if idx < len(c.errors) && c.errors[idx] != nil {
		return egressAuditSnapshot{}, c.errors[idx]
	}
	if len(c.snapshots) == 0 {
		return newEgressAuditSnapshot(time.Unix(100, 0)), nil
	}
	if idx >= len(c.snapshots) {
		idx = len(c.snapshots) - 1
	}
	return c.snapshots[idx], nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
