package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type helperPhase string

const (
	phasePlanned   helperPhase = "planned"
	phaseLaunched  helperPhase = "launched"
	phaseContained helperPhase = "contained"
	phaseExited    helperPhase = "exited"
	phaseFailed    helperPhase = "failed"
	phaseCancelled helperPhase = "cancelled"
)

type metadataEvent struct {
	Phase               helperPhase `json:"phase"`
	EnforcementComplete bool        `json:"enforcement_complete,omitempty"`
}

type runRecord struct {
	Phase    helperPhase `json:"phase"`
	ExitCode int         `json:"exit_code,omitempty"`
	Error    string      `json:"error,omitempty"`
}

type fakeHelperInput struct {
	Metadata []metadataEvent
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Error    string
}

type fakeRunResult struct {
	Record runRecord
	Stdout []byte
	Stderr []byte
}

type sidecarStore struct {
	Dir string
}

func (s sidecarStore) MetadataPath() string {
	return filepath.Join(s.Dir, "metadata.json")
}

func (s sidecarStore) ResultPath() string {
	return filepath.Join(s.Dir, "result.json")
}

func runFakeHelper(ctx context.Context, store sidecarStore, input fakeHelperInput) (fakeRunResult, error) {
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		return fakeRunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		record := runRecord{Phase: phaseCancelled, Error: err.Error()}
		if writeErr := store.writeResultAtomic(record); writeErr != nil {
			return fakeRunResult{Record: record}, writeErr
		}
		_ = os.Remove(store.MetadataPath())
		return fakeRunResult{Record: record}, err
	}
	if err := writeMetadata(store.MetadataPath(), input.Metadata); err != nil {
		return fakeRunResult{}, err
	}
	events, err := readMetadataSidecar(store.MetadataPath())
	if err != nil {
		return fakeRunResult{}, err
	}
	if err := validateMetadata(events); err != nil {
		return fakeRunResult{}, err
	}
	record := runRecord{Phase: phaseExited, ExitCode: input.ExitCode}
	if input.Error != "" {
		record = runRecord{Phase: phaseFailed, Error: input.Error}
	}
	if err := store.writeResultAtomic(record); err != nil {
		return fakeRunResult{Record: record}, err
	}
	_ = os.Remove(store.MetadataPath())
	return fakeRunResult{
		Record: record,
		Stdout: append([]byte(nil), input.Stdout...),
		Stderr: append([]byte(nil), input.Stderr...),
	}, nil
}

func readMetadataSidecar(path string) ([]metadataEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Linux helper metadata sidecar: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var events []metadataEvent
	if err := dec.Decode(&events); err != nil {
		return nil, fmt.Errorf("parse Linux helper metadata sidecar: %w", err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("parse Linux helper metadata sidecar: trailing data")
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("Linux helper metadata sidecar is empty")
	}
	return events, nil
}

func validateMetadata(events []metadataEvent) error {
	want := []helperPhase{phasePlanned, phaseLaunched, phaseContained}
	if len(events) < len(want) {
		return fmt.Errorf("Linux helper metadata missing required phases")
	}
	for i, phase := range want {
		if events[i].Phase != phase {
			return fmt.Errorf("Linux helper metadata phase %d = %q, want %q", i, events[i].Phase, phase)
		}
	}
	if !events[2].EnforcementComplete {
		return fmt.Errorf("Linux helper metadata contained phase arrived before enforcement completed")
	}
	final := events[len(events)-1].Phase
	switch final {
	case phaseExited, phaseFailed, phaseCancelled:
		return nil
	default:
		return fmt.Errorf("Linux helper metadata final phase %q is not terminal", final)
	}
}

func (s sidecarStore) writeResultAtomic(record runRecord) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	temp := s.ResultPath() + ".tmp"
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, s.ResultPath())
}

func writeMetadata(path string, events []metadataEvent) error {
	data, err := json.Marshal(events)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func TestValidateMetadataCoversRequiredPhases(t *testing.T) {
	for _, final := range []helperPhase{phaseExited, phaseFailed, phaseCancelled} {
		if err := validateMetadata(validMetadataEvents(final)); err != nil {
			t.Fatalf("validateMetadata(%q): %v", final, err)
		}
	}
}

func TestValidateMetadataRejectsContainedBeforeEnforcement(t *testing.T) {
	events := validMetadataEvents(phaseExited)
	events[2].EnforcementComplete = false

	err := validateMetadata(events)
	if err == nil || !strings.Contains(err.Error(), "before enforcement") {
		t.Fatalf("err = %v, want before-enforcement rejection", err)
	}
}

func TestReadMetadataSidecarRejectsMissingAndMalformed(t *testing.T) {
	store := sidecarStore{Dir: t.TempDir()}
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "malformed", data: `[{`},
		{name: "unknown field", data: `[{"phase":"planned","extra":true}]`},
		{name: "trailing data", data: `[{"phase":"planned"}] []`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(store.MetadataPath(), []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readMetadataSidecar(store.MetadataPath()); err == nil {
				t.Fatal("readMetadataSidecar accepted malformed metadata")
			}
		})
	}

	if _, err := readMetadataSidecar(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("readMetadataSidecar accepted missing metadata")
	}
}

func TestRunFakeHelperPreservesRawStreamsAndWritesExitedResult(t *testing.T) {
	store := sidecarStore{Dir: t.TempDir()}
	stdout := []byte("harness stdout\n\x00raw\n")
	stderr := []byte("harness stderr\n\x00raw\n")

	result, err := runFakeHelper(context.Background(), store, fakeHelperInput{
		Metadata: validMetadataEvents(phaseExited),
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: 7,
	})
	if err != nil {
		t.Fatalf("runFakeHelper: %v", err)
	}
	if !bytes.Equal(result.Stdout, stdout) {
		t.Fatalf("Stdout = %q, want %q", result.Stdout, stdout)
	}
	if !bytes.Equal(result.Stderr, stderr) {
		t.Fatalf("Stderr = %q, want %q", result.Stderr, stderr)
	}
	record := readRunRecord(t, store.ResultPath())
	if record.Phase != phaseExited || record.ExitCode != 7 || record.Error != "" {
		t.Fatalf("record = %+v, want exited code 7", record)
	}
	if _, err := os.Stat(store.MetadataPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata sidecar stat err = %v, want removed", err)
	}
}

func TestRunFakeRootHelperAgentUserMetadataAfterEnforcement(t *testing.T) {
	store := sidecarStore{Dir: t.TempDir()}
	result, err := runFakeHelper(context.Background(), store, fakeHelperInput{
		Metadata: validMetadataEvents(phaseExited),
		Stdout:   []byte("agent-user stdout\n"),
		Stderr:   []byte("agent-user stderr\n"),
	})
	if err != nil {
		t.Fatalf("runFakeHelper: %v", err)
	}
	if result.Record.Phase != phaseExited {
		t.Fatalf("record = %+v, want exited", result.Record)
	}
	record := readRunRecord(t, store.ResultPath())
	if record.Phase != phaseExited {
		t.Fatalf("stored record = %+v, want exited", record)
	}
}

func TestRunFakeHelperFailurePreservesRawStderrSeparation(t *testing.T) {
	store := sidecarStore{Dir: t.TempDir()}
	const helperErr = "helper setup failed"
	stderr := []byte("harness stderr only\n")

	result, err := runFakeHelper(context.Background(), store, fakeHelperInput{
		Metadata: validMetadataEvents(phaseFailed),
		Stdout:   []byte("harness stdout only\n"),
		Stderr:   stderr,
		Error:    helperErr,
	})
	if err != nil {
		t.Fatalf("runFakeHelper: %v", err)
	}
	if !bytes.Equal(result.Stderr, stderr) {
		t.Fatalf("Stderr = %q, want %q", result.Stderr, stderr)
	}
	if bytes.Contains(result.Stdout, []byte(helperErr)) {
		t.Fatalf("helper diagnostic leaked into raw stdout: %q", result.Stdout)
	}
	record := readRunRecord(t, store.ResultPath())
	if record.Phase != phaseFailed || record.Error != helperErr {
		t.Fatalf("record = %+v, want failed helper diagnostic", record)
	}
}

func TestRunFakeHelperCancellationWritesAtomicResultAndRemovesSidecar(t *testing.T) {
	store := sidecarStore{Dir: t.TempDir()}
	if err := os.WriteFile(store.MetadataPath(), []byte(`[{"phase":"planned"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ResultPath()+".tmp", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := runFakeHelper(ctx, store, fakeHelperInput{Metadata: validMetadataEvents(phaseCancelled)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if result.Record.Phase != phaseCancelled {
		t.Fatalf("result record = %+v, want cancelled", result.Record)
	}
	record := readRunRecord(t, store.ResultPath())
	if record.Phase != phaseCancelled {
		t.Fatalf("record = %+v, want cancelled", record)
	}
	for _, path := range []string{store.MetadataPath(), store.ResultPath() + ".tmp"} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s stat err = %v, want removed", path, err)
		}
	}
}

func validMetadataEvents(final helperPhase) []metadataEvent {
	return []metadataEvent{
		{Phase: phasePlanned},
		{Phase: phaseLaunched},
		{Phase: phaseContained, EnforcementComplete: true},
		{Phase: final},
	}
}

func readRunRecord(t *testing.T, path string) runRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record runRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}
