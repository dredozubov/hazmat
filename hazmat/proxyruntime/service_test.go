package proxyruntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"hazmat/sessionbackend"
)

var (
	errServiceHealth  = errors.New("health failed")
	errServiceCleanup = errors.New("cleanup failed")
	errStartResidue   = errors.New("start failed after residue")
)

func TestServiceRunnerSuccessReadinessAttachStopAndRedaction(t *testing.T) {
	var order []string
	store := &fakeServiceStore{order: &order}
	credentials := &fakeServiceCredentials{order: &order}
	instance := &fakeServiceInstance{order: &order}
	service := &fakeService{order: &order, instance: instance}
	var events []Event

	result, err := (ServiceRunner{
		Store:       store,
		Credentials: credentials,
		Events:      func(event Event) { events = append(events, event) },
	}).Run(context.Background(), validServiceRequest(), service)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.MetadataRecorded || !result.CredentialMaterialized || !result.Ready ||
		!result.Attached || !result.Stopped || !result.CredentialCleaned {
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
	ready := firstServiceEvent(t, events, "service:ready")
	if ready.Attributes["session_token"] != RedactedValue {
		t.Fatalf("ready session_token = %q, want redacted; event=%+v", ready.Attributes["session_token"], ready)
	}
	if containsEventValue(events, "secret-token") {
		t.Fatalf("events leaked session token: %+v", events)
	}
}

func TestServiceRunnerRejectsUnsafeRequestsBeforeSideEffects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ServiceRequest)
		reason ServiceDenialReason
	}{
		{"profile import", func(req *ServiceRequest) { req.Features.ProfileImport = true }, ServiceDenialProfileImport},
		{"persistent daemon", func(req *ServiceRequest) { req.Features.PersistentDaemon = true }, ServiceDenialPersistentDaemon},
		{"browser automation", func(req *ServiceRequest) { req.Features.BrowserAutomation = true }, ServiceDenialBrowserAutomation},
		{"integration env", func(req *ServiceRequest) { req.Features.IntegrationEnv = true }, ServiceDenialIntegrationEnv},
		{"stdio attach", func(req *ServiceRequest) { req.Attach.Kind = AttachKindStdio }, ServiceDenialNonLocalBind},
		{"remote http attach", func(req *ServiceRequest) { req.Attach.Kind = AttachKindRemoteHTTP }, ServiceDenialNonLocalBind},
		{"wildcard local http", func(req *ServiceRequest) { req.Attach.Address = "0.0.0.0:8080" }, ServiceDenialNonLocalBind},
		{"lan local http", func(req *ServiceRequest) { req.Attach.Address = "192.168.1.10:8080" }, ServiceDenialNonLocalBind},
		{"missing local http host", func(req *ServiceRequest) { req.Attach.Address = ":8080" }, ServiceDenialNonLocalBind},
		{"localhost without token", func(req *ServiceRequest) { req.Attach.SessionToken = "" }, ServiceDenialMissingToken},
		{"native container", func(req *ServiceRequest) {
			req.Backend = sessionbackend.KindDarwinNative
			req.RequiresContainer = true
		}, ServiceDenialNativeContainer},
		{"untyped credential", func(req *ServiceRequest) { req.Credentials.Mode = ServiceCredentialUntyped }, ServiceDenialUntypedCredential},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var order []string
			req := validServiceRequest()
			tc.mutate(&req)
			store := &fakeServiceStore{order: &order}
			credentials := &fakeServiceCredentials{order: &order}
			service := &fakeService{order: &order, instance: &fakeServiceInstance{order: &order}}

			result, err := (ServiceRunner{Store: store, Credentials: credentials}).Run(context.Background(), req, service)
			if err == nil {
				t.Fatal("Run succeeded, want unsupported request error")
			}
			var unsupported UnsupportedServiceRequestError
			if !errors.As(err, &unsupported) || !unsupported.Has(tc.reason) {
				t.Fatalf("error = %v, want denial %s", err, tc.reason)
			}
			if !result.Rejected || store.planned || service.started || credentials.materialized {
				t.Fatalf("unsafe request had side effects: result=%+v planned=%v started=%v materialized=%v order=%v", result, store.planned, service.started, credentials.materialized, order)
			}
		})
	}
}

func TestServiceRunnerCleansStaleResidueBeforeStart(t *testing.T) {
	var order []string
	store := &fakeServiceStore{
		order: &order,
		residues: []ServiceResidue{{
			SessionID:         "stale-1",
			ServiceResidue:    true,
			CredentialResidue: true,
			AttachResidue:     true,
		}},
	}
	credentials := &fakeServiceCredentials{order: &order}
	service := &fakeService{order: &order, instance: &fakeServiceInstance{order: &order}}

	result, err := (ServiceRunner{Store: store, Credentials: credentials}).Run(context.Background(), validServiceRequest(), service)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ResidueRecovered != 1 {
		t.Fatalf("ResidueRecovered = %d, want 1", result.ResidueRecovered)
	}
	assertOrderBefore(t, order, "cleanup-residue:stale-1", "record-planned")
	assertOrderBefore(t, order, "cleanup-residue:stale-1", "start")
}

func TestServiceRunnerStopsBeforeStartWhenStaleResidueCleanupFails(t *testing.T) {
	var order []string
	store := &fakeServiceStore{
		order:      &order,
		cleanupErr: errServiceCleanup,
		residues: []ServiceResidue{{
			SessionID:      "stale-2",
			ServiceResidue: true,
		}},
	}
	credentials := &fakeServiceCredentials{order: &order}
	service := &fakeService{order: &order, instance: &fakeServiceInstance{order: &order}}

	result, err := (ServiceRunner{Store: store, Credentials: credentials}).Run(context.Background(), validServiceRequest(), service)
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

func TestServiceRunnerCleansUpAfterHealthFailure(t *testing.T) {
	var order []string
	store := &fakeServiceStore{order: &order}
	credentials := &fakeServiceCredentials{order: &order}
	instance := &fakeServiceInstance{order: &order, healthErr: errServiceHealth}
	service := &fakeService{order: &order, instance: instance}

	result, err := (ServiceRunner{Store: store, Credentials: credentials}).Run(context.Background(), validServiceRequest(), service)
	if err == nil || !strings.Contains(err.Error(), errServiceHealth.Error()) {
		t.Fatalf("Run error = %v, want health failure", err)
	}
	if !result.ServiceStarted || result.Ready || result.Attached || !result.Stopped || !result.CredentialCleaned {
		t.Fatalf("unexpected result after health failure: %+v", result)
	}
	assertOrderBefore(t, order, "health", "stop")
	assertOrderBefore(t, order, "stop", "cleanup-credentials")
}

func TestServiceRunnerCleansStartFailureWithResidue(t *testing.T) {
	var order []string
	store := &fakeServiceStore{order: &order}
	credentials := &fakeServiceCredentials{order: &order}
	instance := &fakeServiceInstance{order: &order}
	service := &fakeService{order: &order, instance: instance, startErr: errStartResidue}

	result, err := (ServiceRunner{Store: store, Credentials: credentials}).Run(context.Background(), validServiceRequest(), service)
	if err == nil || !strings.Contains(err.Error(), errStartResidue.Error()) {
		t.Fatalf("Run error = %v, want start residue failure", err)
	}
	if !result.ServiceStarted || !result.Stopped || !result.CredentialCleaned {
		t.Fatalf("start residue was not cleaned: %+v", result)
	}
	assertOrderBefore(t, order, "start", "stop")
	assertOrderBefore(t, order, "stop", "cleanup-credentials")
}

func validServiceRequest() ServiceRequest {
	return ServiceRequest{
		ServiceKind:       ServiceKindProxyAPI,
		SessionID:         "session-1",
		ProxyKind:         ProxyKindLLMHTTP,
		Downstream:        DownstreamIdentity{ID: "external-facade"},
		Backend:           sessionbackend.KindDockerSandbox,
		RequiresContainer: true,
		Attach: ServiceAttach{
			Kind:         AttachKindLocalHTTP,
			Address:      "127.0.0.1:0",
			SessionToken: "secret-token",
		},
		Credentials: ServiceCredentialPlan{
			Mode: ServiceCredentialTyped,
			IDs:  []string{"provider.openai.api-key"},
		},
	}
}

type fakeServiceStore struct {
	order           *[]string
	residues        []ServiceResidue
	cleanupErr      error
	planned         bool
	ready           bool
	stopped         bool
	cleanupFailures []string
}

func (s *fakeServiceStore) ListResidue(context.Context) ([]ServiceResidue, error) {
	s.append("list-residue")
	return append([]ServiceResidue(nil), s.residues...), nil
}

func (s *fakeServiceStore) CleanupResidue(_ context.Context, residue ServiceResidue) error {
	s.append("cleanup-residue:" + residue.SessionID)
	return s.cleanupErr
}

func (s *fakeServiceStore) RecordPlanned(context.Context, ServiceMetadata) error {
	s.append("record-planned")
	s.planned = true
	return nil
}

func (s *fakeServiceStore) RecordReady(_ context.Context, _ string, _ ServiceAttach) error {
	s.append("record-ready")
	s.ready = true
	return nil
}

func (s *fakeServiceStore) RecordStopped(context.Context, string) error {
	s.append("record-stopped")
	s.stopped = true
	return nil
}

func (s *fakeServiceStore) RecordCleanupFailure(_ context.Context, sessionID string, _ error) error {
	s.append("record-cleanup-failure:" + sessionID)
	s.cleanupFailures = append(s.cleanupFailures, sessionID)
	return nil
}

func (s *fakeServiceStore) append(event string) {
	*s.order = append(*s.order, event)
}

type fakeServiceCredentials struct {
	order        *[]string
	materialized bool
	cleaned      bool
}

func (c *fakeServiceCredentials) Materialize(context.Context, string, ServiceCredentialPlan) error {
	*c.order = append(*c.order, "materialize-credentials")
	c.materialized = true
	return nil
}

func (c *fakeServiceCredentials) Cleanup(context.Context, string, ServiceCredentialPlan) error {
	*c.order = append(*c.order, "cleanup-credentials")
	c.cleaned = true
	return nil
}

type fakeService struct {
	order    *[]string
	instance *fakeServiceInstance
	startErr error
	started  bool
}

func (s *fakeService) Start(context.Context, ServiceStartRequest) (ServiceInstance, error) {
	*s.order = append(*s.order, "start")
	s.started = true
	return s.instance, s.startErr
}

type fakeServiceInstance struct {
	order     *[]string
	healthErr error
	attachErr error
	stopErr   error
}

func (i *fakeServiceInstance) Health(context.Context) error {
	*i.order = append(*i.order, "health")
	return i.healthErr
}

func (i *fakeServiceInstance) Attach(context.Context, ServiceAttach) error {
	*i.order = append(*i.order, "attach")
	return i.attachErr
}

func (i *fakeServiceInstance) Stop(context.Context) error {
	*i.order = append(*i.order, "stop")
	return i.stopErr
}

func firstServiceEvent(t *testing.T, events []Event, operation string) Event {
	t.Helper()
	for _, event := range events {
		if event.Operation == operation {
			return event
		}
	}
	t.Fatalf("event %q not found in %+v", operation, events)
	return Event{}
}

func containsEventValue(events []Event, value string) bool {
	for _, event := range events {
		for _, got := range event.Attributes {
			if strings.Contains(got, value) {
				return true
			}
		}
	}
	return false
}

func assertOrderBefore(t *testing.T, order []string, before, after string) {
	t.Helper()
	beforeIndex := -1
	afterIndex := -1
	for i, value := range order {
		if value == before {
			beforeIndex = i
		}
		if value == after {
			afterIndex = i
		}
	}
	if beforeIndex == -1 || afterIndex == -1 || beforeIndex >= afterIndex {
		t.Fatalf("order %v does not place %q before %q", order, before, after)
	}
}
