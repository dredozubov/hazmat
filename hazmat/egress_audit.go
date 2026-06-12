package hazmat

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type egressAuditEndpoint struct {
	Protocol string
	Host     string
	Port     string
}

func (e egressAuditEndpoint) key() string {
	return strings.ToLower(e.Protocol) + "\x00" + strings.ToLower(e.Host) + "\x00" + e.Port
}

func (e egressAuditEndpoint) label() string {
	protocol := strings.ToLower(strings.TrimSpace(e.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	host := strings.TrimSpace(e.Host)
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	if e.Port == "" {
		return protocol + " " + host
	}
	return protocol + " " + host + ":" + e.Port
}

type egressAuditSnapshot struct {
	CapturedAt time.Time
	Endpoints  map[string]egressAuditEndpoint
}

func newEgressAuditSnapshot(capturedAt time.Time) egressAuditSnapshot {
	return egressAuditSnapshot{
		CapturedAt: capturedAt,
		Endpoints:  make(map[string]egressAuditEndpoint),
	}
}

func (s egressAuditSnapshot) add(endpoint egressAuditEndpoint) {
	if strings.TrimSpace(endpoint.Host) == "" {
		return
	}
	if strings.TrimSpace(endpoint.Protocol) == "" {
		endpoint.Protocol = "tcp"
	}
	s.Endpoints[endpoint.key()] = endpoint
}

type egressAuditCollector interface {
	Snapshot() (egressAuditSnapshot, error)
}

type lsofEgressAuditCollector struct {
	user   string
	now    func() time.Time
	output func(string, ...string) (string, error)
}

var newEgressAuditCollector = func() egressAuditCollector {
	return lsofEgressAuditCollector{
		user:   agentUser,
		now:    time.Now,
		output: commandStdout,
	}
}

var egressAuditPollInterval = 500 * time.Millisecond

func (c lsofEgressAuditCollector) Snapshot() (egressAuditSnapshot, error) {
	now := c.now
	if now == nil {
		now = time.Now
	}
	output := c.output
	if output == nil {
		output = commandStdout
	}
	user := strings.TrimSpace(c.user)
	if user == "" {
		user = agentUser
	}
	raw, err := output(hostLsofPath,
		"-nP",
		"-a",
		"-u", user,
		"-iTCP",
		"-sTCP:ESTABLISHED",
	)
	if err != nil && strings.TrimSpace(raw) == "" {
		return newEgressAuditSnapshot(now()), nil
	}
	snapshot := parseLSOFEgressAuditSnapshot(raw, now())
	if err != nil {
		return snapshot, fmt.Errorf("lsof egress snapshot: %w", err)
	}
	return snapshot, nil
}

func runAuditInstallExec(w io.Writer, collector egressAuditCollector, run func() error) error {
	if w == nil {
		w = io.Discard
	}
	if collector == nil {
		collector = newEgressAuditCollector()
	}
	if run == nil {
		return fmt.Errorf("audit-install requires a session runner")
	}

	baseline, err := collector.Snapshot()
	if err != nil {
		return fmt.Errorf("audit-install baseline egress snapshot: %w", err)
	}

	observed := newEgressAuditSnapshot(baseline.CapturedAt)
	var (
		mu       sync.Mutex
		pollErrs []error
	)
	merge := func(snapshot egressAuditSnapshot) {
		mu.Lock()
		defer mu.Unlock()
		for key, endpoint := range snapshot.Endpoints {
			observed.Endpoints[key] = endpoint
		}
		if snapshot.CapturedAt.After(observed.CapturedAt) {
			observed.CapturedAt = snapshot.CapturedAt
		}
	}
	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		pollErrs = append(pollErrs, err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(egressAuditPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				snapshot, err := collector.Snapshot()
				if err != nil {
					recordErr(err)
					continue
				}
				merge(snapshot)
			case <-done:
				return
			}
		}
	}()

	runErr := run()
	close(done)
	wg.Wait()

	final, finalErr := collector.Snapshot()
	if finalErr != nil {
		recordErr(finalErr)
	} else {
		merge(final)
	}

	mu.Lock()
	report := buildAuditInstallReport(baseline, observed, pollErrs)
	telemetryErr := errors.Join(pollErrs...)
	mu.Unlock()
	writeAuditInstallReport(w, report)
	if runErr != nil {
		return runErr
	}
	if telemetryErr != nil {
		return fmt.Errorf("audit-install telemetry incomplete: %w", telemetryErr)
	}
	return nil
}

type auditInstallReport struct {
	BaselineCount int
	NewEndpoints  []egressAuditEndpoint
	Review        []egressAuditEndpoint
	Known         []egressAuditEndpoint
	Warnings      []string
}

func buildAuditInstallReport(baseline, observed egressAuditSnapshot, warnings []error) auditInstallReport {
	report := auditInstallReport{BaselineCount: len(baseline.Endpoints)}
	for key, endpoint := range observed.Endpoints {
		if _, existed := baseline.Endpoints[key]; existed {
			continue
		}
		report.NewEndpoints = append(report.NewEndpoints, endpoint)
		if auditInstallKnownEndpoint(endpoint) {
			report.Known = append(report.Known, endpoint)
		} else {
			report.Review = append(report.Review, endpoint)
		}
	}
	sortEgressAuditEndpoints(report.NewEndpoints)
	sortEgressAuditEndpoints(report.Known)
	sortEgressAuditEndpoints(report.Review)
	for _, err := range warnings {
		if err != nil {
			report.Warnings = append(report.Warnings, err.Error())
		}
	}
	sort.Strings(report.Warnings)
	return report
}

func writeAuditInstallReport(w io.Writer, report auditInstallReport) {
	fmt.Fprintln(w, "hazmat: audit-install egress report")
	fmt.Fprintf(w, "  baseline endpoints: %d\n", report.BaselineCount)
	fmt.Fprintf(w, "  new endpoints observed: %d\n", len(report.NewEndpoints))
	if len(report.Known) > 0 {
		fmt.Fprintln(w, "  Known package endpoints:")
		for _, endpoint := range report.Known {
			fmt.Fprintf(w, "    - %s\n", endpoint.label())
		}
	}
	if len(report.Review) > 0 {
		fmt.Fprintln(w, "  Review endpoints:")
		for _, endpoint := range report.Review {
			fmt.Fprintf(w, "    - %s\n", endpoint.label())
		}
	}
	if len(report.NewEndpoints) == 0 {
		fmt.Fprintln(w, "  No new established TCP endpoints were observed while the command ran.")
	}
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "  Telemetry warnings:")
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "    - %s\n", warning)
		}
	}
	fmt.Fprintln(w, "  Result: observational only; package-manager behavior was not blocked by this report.")
}

func parseLSOFEgressAuditSnapshot(raw string, capturedAt time.Time) egressAuditSnapshot {
	snapshot := newEgressAuditSnapshot(capturedAt)
	for _, line := range strings.Split(raw, "\n") {
		endpoint, ok := parseLSOFEgressAuditLine(line)
		if ok {
			snapshot.add(endpoint)
		}
	}
	return snapshot
}

func parseLSOFEgressAuditLine(line string) (egressAuditEndpoint, bool) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.Contains(line, "->") {
		return egressAuditEndpoint{}, false
	}
	tcp := strings.Index(line, "TCP ")
	if tcp < 0 {
		return egressAuditEndpoint{}, false
	}
	name := strings.TrimSpace(line[tcp+len("TCP "):])
	arrow := strings.Index(name, "->")
	if arrow < 0 {
		return egressAuditEndpoint{}, false
	}
	remote := strings.TrimSpace(name[arrow+len("->"):])
	if sp := strings.IndexAny(remote, " \t"); sp >= 0 {
		remote = remote[:sp]
	}
	host, port, ok := splitLSOFRemoteEndpoint(remote)
	if !ok {
		return egressAuditEndpoint{}, false
	}
	return egressAuditEndpoint{Protocol: "tcp", Host: host, Port: port}, true
}

func splitLSOFRemoteEndpoint(remote string) (string, string, bool) {
	remote = strings.TrimSpace(remote)
	if remote == "" || remote == "*" {
		return "", "", false
	}
	if strings.HasPrefix(remote, "[") {
		end := strings.LastIndex(remote, "]:")
		if end < 0 {
			return "", "", false
		}
		host := strings.TrimPrefix(remote[:end+1], "[")
		host = strings.TrimSuffix(host, "]")
		return host, remote[end+len("]:"):], true
	}
	idx := strings.LastIndex(remote, ":")
	if idx <= 0 || idx == len(remote)-1 {
		return "", "", false
	}
	return remote[:idx], remote[idx+1:], true
}

func sortEgressAuditEndpoints(values []egressAuditEndpoint) {
	sort.Slice(values, func(i, j int) bool {
		return values[i].key() < values[j].key()
	})
}

func auditInstallKnownEndpoint(endpoint egressAuditEndpoint) bool {
	host := strings.ToLower(strings.Trim(endpoint.Host, "[]"))
	for _, known := range []string{
		"registry.npmjs.org",
		"npm.pkg.github.com",
		"github.com",
		"api.github.com",
		"objects.githubusercontent.com",
		"codeload.github.com",
		"raw.githubusercontent.com",
		"pypi.org",
		"files.pythonhosted.org",
		"crates.io",
		"index.crates.io",
		"static.crates.io",
		"proxy.golang.org",
		"sum.golang.org",
		"rubygems.org",
		"repo.maven.apache.org",
		"plugins.gradle.org",
	} {
		if host == known || strings.HasSuffix(host, "."+known) {
			return true
		}
	}
	return false
}
