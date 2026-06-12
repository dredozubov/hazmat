package serviceharness

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

var (
	errCrash        = errors.New("service crashed")
	errHang         = errors.New("service health check hung")
	errCleanup      = errors.New("cleanup failed")
	errStartResidue = errors.New("start failed after residue")
)

func TestRunnerSuccessReadinessAttachStopAndRedaction(t *testing.T) {
	order := []string{}
	store := &fakeStore{order: &order}
	creds := &fakeCredentials{order: &order}
	handle := &fakeInstance{order: &order}
	service := &fakeService{order: &order, handle: handle}
	logs := &fakeLogger{}
	req := validRequest()

	result, err := Runner{Store: store, Credentials: creds, Logger: logs}.Run(context.Background(), req, service)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.MetadataRecorded || !result.CredentialMaterialized || !result.Ready || !result.Attached || !result.Stopped || !result.CredentialCleaned {
		t.Fatalf("result missing success phases: %+v", result)
	}
	wantOrder := []string{
		"list-residue",
		"record-planned",
		"materialize-credentials",
		"start",
		"health",
		"record-ready",
		"attach",
		"stop",
		"record-stopped",
		"cleanup-credentials",
	}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}
	if logs.containsValue("secret-token") {
		t.Fatalf("logs leaked session token: %#v", logs.events)
	}
	ready := logs.first("ready")
	if ready.Fields["session_token"] != "[redacted]" {
		t.Fatalf("ready session_token = %q, want redacted; event=%+v", ready.Fields["session_token"], ready)
	}
}

func TestRunnerCleansStaleResidueBeforeStart(t *testing.T) {
	order := []string{}
	store := &fakeStore{
		order: &order,
		residues: []Residue{{
			SessionID:         "stale-1",
			ServiceResidue:    true,
			CredentialResidue: true,
			AttachResidue:     true,
		}},
	}
	creds := &fakeCredentials{order: &order}
	service := &fakeService{order: &order, handle: &fakeInstance{order: &order}}

	result, err := Runner{Store: store, Credentials: creds}.Run(context.Background(), validRequest(), service)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ResidueRecovered != 1 {
		t.Fatalf("ResidueRecovered = %d, want 1", result.ResidueRecovered)
	}
	assertBefore(t, order, "cleanup-residue:stale-1", "record-planned")
	assertBefore(t, order, "cleanup-residue:stale-1", "start")
}

func TestRunnerStopsBeforeStartWhenStaleResidueCleanupFails(t *testing.T) {
	order := []string{}
	store := &fakeStore{
		order:      &order,
		cleanupErr: errCleanup,
		residues: []Residue{{
			SessionID:      "stale-2",
			ServiceResidue: true,
		}},
	}
	creds := &fakeCredentials{order: &order}
	service := &fakeService{order: &order, handle: &fakeInstance{order: &order}}

	result, err := Runner{Store: store, Credentials: creds}.Run(context.Background(), validRequest(), service)
	if err == nil {
		t.Fatal("Run succeeded, want stale cleanup failure")
	}
	if result.ServiceStarted || store.planned || service.started {
		t.Fatalf("runner started after stale cleanup failure: result=%+v planned=%v started=%v order=%v", result, store.planned, service.started, order)
	}
	if len(store.cleanupFailures) != 1 || store.cleanupFailures[0] != "stale-2" {
		t.Fatalf("cleanupFailures = %#v, want stale-2", store.cleanupFailures)
	}
}

func TestRunnerRejectsUnsafeRequestsBeforeSideEffects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Request)
		reason DenialReason
	}{
		{"host docker socket", func(req *Request) { req.Features.HostDockerSocket = true }, DenialHostDockerSocket},
		{"profile import", func(req *Request) { req.Features.ProfileImport = true }, DenialProfileImport},
		{"persistent daemon", func(req *Request) { req.Features.PersistentDaemon = true }, DenialPersistentDaemon},
		{"browser automation", func(req *Request) { req.Features.BrowserAutomation = true }, DenialBrowserAutomation},
		{"integration env", func(req *Request) { req.Features.IntegrationEnv = true }, DenialIntegrationEnv},
		{"lan bind", func(req *Request) { req.Attach.Kind = AttachLANPort }, DenialLANBind},
		{"localhost without token", func(req *Request) { req.Attach.SessionToken = "" }, DenialMissingPortToken},
		{"native container", func(req *Request) { req.Backend = BackendNative; req.RequiresContainer = true }, DenialNativeContainer},
		{"untyped credential", func(req *Request) { req.Credentials.Mode = CredentialUntyped }, DenialUntypedCredential},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order := []string{}
			req := validRequest()
			tc.mutate(&req)
			store := &fakeStore{order: &order}
			creds := &fakeCredentials{order: &order}
			service := &fakeService{order: &order, handle: &fakeInstance{order: &order}}

			result, err := Runner{Store: store, Credentials: creds}.Run(context.Background(), req, service)
			if err == nil {
				t.Fatal("Run succeeded, want unsupported request error")
			}
			var unsupported UnsupportedRequestError
			if !errors.As(err, &unsupported) || !unsupported.Has(tc.reason) {
				t.Fatalf("error = %v, want denial %s", err, tc.reason)
			}
			if !result.Rejected || store.planned || service.started || creds.materialized {
				t.Fatalf("unsafe request had side effects: result=%+v planned=%v started=%v materialized=%v order=%v", result, store.planned, service.started, creds.materialized, order)
			}
		})
	}
}

func TestRunnerCleansUpAfterHealthHang(t *testing.T) {
	order := []string{}
	store := &fakeStore{order: &order}
	creds := &fakeCredentials{order: &order}
	handle := &fakeInstance{order: &order, healthErr: errHang}
	service := &fakeService{order: &order, handle: handle}

	result, err := Runner{Store: store, Credentials: creds}.Run(context.Background(), validRequest(), service)
	if err == nil || !strings.Contains(err.Error(), errHang.Error()) {
		t.Fatalf("Run error = %v, want health hang", err)
	}
	if !result.ServiceStarted || result.Ready || result.Attached || !result.Stopped || !result.CredentialCleaned {
		t.Fatalf("unexpected result after health hang: %+v", result)
	}
	assertBefore(t, order, "health", "stop")
	assertBefore(t, order, "stop", "cleanup-credentials")
}

func TestRunnerRecordsCleanupFailureAfterCrash(t *testing.T) {
	order := []string{}
	store := &fakeStore{order: &order}
	creds := &fakeCredentials{order: &order}
	handle := &fakeInstance{order: &order, attachErr: errCrash, stopErr: errCleanup}
	service := &fakeService{order: &order, handle: handle}

	result, err := Runner{Store: store, Credentials: creds}.Run(context.Background(), validRequest(), service)
	if err == nil || !strings.Contains(err.Error(), errCrash.Error()) || !strings.Contains(err.Error(), errCleanup.Error()) {
		t.Fatalf("Run error = %v, want crash plus cleanup failure", err)
	}
	if result.CleanupFailures != 1 || len(store.cleanupFailures) != 1 || store.cleanupFailures[0] != validRequest().SessionID {
		t.Fatalf("cleanup failure accounting wrong: result=%+v store=%#v", result, store.cleanupFailures)
	}
	if !result.CredentialCleaned {
		t.Fatalf("credentials were not cleaned after crash cleanup failure: %+v", result)
	}
}

func TestRunnerCleansStartFailureWithResidue(t *testing.T) {
	order := []string{}
	store := &fakeStore{order: &order}
	creds := &fakeCredentials{order: &order}
	handle := &fakeInstance{order: &order}
	service := &fakeService{order: &order, handle: handle, startErr: errStartResidue}

	result, err := Runner{Store: store, Credentials: creds}.Run(context.Background(), validRequest(), service)
	if err == nil || !strings.Contains(err.Error(), errStartResidue.Error()) {
		t.Fatalf("Run error = %v, want start residue failure", err)
	}
	if !result.ServiceStarted || !result.Stopped || !result.CredentialCleaned {
		t.Fatalf("start residue was not cleaned: %+v", result)
	}
	assertBefore(t, order, "start", "stop")
	assertBefore(t, order, "stop", "cleanup-credentials")
}

func validRequest() Request {
	return Request{
		AdapterID:         "fake-service",
		SessionID:         "session-1",
		Backend:           BackendDockerSandbox,
		RequiresContainer: true,
		Attach: AttachPlan{
			Kind:         AttachLocalhostPort,
			SessionToken: "secret-token",
		},
		Credentials: CredentialPlan{
			Mode: CredentialTyped,
			IDs:  []string{"provider.openai.api-key"},
		},
	}
}

type fakeStore struct {
	order           *[]string
	residues        []Residue
	cleanupErr      error
	planned         bool
	ready           bool
	stopped         bool
	cleanupFailures []string
}

func (s *fakeStore) ListResidue(context.Context) ([]Residue, error) {
	s.append("list-residue")
	return append([]Residue(nil), s.residues...), nil
}

func (s *fakeStore) CleanupResidue(_ context.Context, residue Residue) error {
	s.append("cleanup-residue:" + residue.SessionID)
	return s.cleanupErr
}

func (s *fakeStore) RecordPlanned(context.Context, Metadata) error {
	s.append("record-planned")
	s.planned = true
	return nil
}

func (s *fakeStore) RecordReady(_ context.Context, _ string, _ AttachPlan) error {
	s.append("record-ready")
	s.ready = true
	return nil
}

func (s *fakeStore) RecordStopped(context.Context, string) error {
	s.append("record-stopped")
	s.stopped = true
	return nil
}

func (s *fakeStore) RecordCleanupFailure(_ context.Context, sessionID string, _ error) error {
	s.append("record-cleanup-failure:" + sessionID)
	s.cleanupFailures = append(s.cleanupFailures, sessionID)
	return nil
}

func (s *fakeStore) append(event string) {
	*s.order = append(*s.order, event)
}

type fakeCredentials struct {
	order        *[]string
	materialized bool
	cleaned      bool
}

func (c *fakeCredentials) Materialize(context.Context, string, CredentialPlan) error {
	*c.order = append(*c.order, "materialize-credentials")
	c.materialized = true
	return nil
}

func (c *fakeCredentials) Cleanup(context.Context, string, CredentialPlan) error {
	*c.order = append(*c.order, "cleanup-credentials")
	c.cleaned = true
	return nil
}

type fakeService struct {
	order    *[]string
	handle   *fakeInstance
	startErr error
	started  bool
}

func (s *fakeService) Start(context.Context, StartRequest) (Instance, error) {
	*s.order = append(*s.order, "start")
	s.started = true
	return s.handle, s.startErr
}

type fakeInstance struct {
	order     *[]string
	healthErr error
	attachErr error
	stopErr   error
}

func (i *fakeInstance) Health(context.Context) error {
	*i.order = append(*i.order, "health")
	return i.healthErr
}

func (i *fakeInstance) Attach(context.Context, AttachPlan) error {
	*i.order = append(*i.order, "attach")
	return i.attachErr
}

func (i *fakeInstance) Stop(context.Context) error {
	*i.order = append(*i.order, "stop")
	return i.stopErr
}

type fakeLogger struct {
	events []Event
}

func (l *fakeLogger) Event(event Event) {
	l.events = append(l.events, event)
}

func (l *fakeLogger) containsValue(value string) bool {
	for _, event := range l.events {
		for _, got := range event.Fields {
			if strings.Contains(got, value) {
				return true
			}
		}
	}
	return false
}

func (l *fakeLogger) first(phase string) Event {
	for _, event := range l.events {
		if event.Phase == phase {
			return event
		}
	}
	return Event{}
}

func assertBefore(t *testing.T, order []string, before, after string) {
	t.Helper()
	beforeIndex := -1
	afterIndex := -1
	for i, event := range order {
		if event == before && beforeIndex == -1 {
			beforeIndex = i
		}
		if event == after && afterIndex == -1 {
			afterIndex = i
		}
	}
	if beforeIndex == -1 || afterIndex == -1 || beforeIndex >= afterIndex {
		t.Fatalf("order %q before %q failed in %#v", before, after, order)
	}
}
