package serviceharness

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Backend string

const (
	BackendNative        Backend = "native"
	BackendDockerSandbox Backend = "docker-sandbox"
	BackendVM            Backend = "vm"
)

type AttachKind string

const (
	AttachStdio         AttachKind = "stdio"
	AttachUnixSocket    AttachKind = "uds"
	AttachLocalhostPort AttachKind = "localhost-port"
	AttachLANPort       AttachKind = "lan-port"
)

type CredentialMode string

const (
	CredentialNone    CredentialMode = "none"
	CredentialTyped   CredentialMode = "typed"
	CredentialUntyped CredentialMode = "untyped"
)

type DenialReason string

const (
	DenialHostDockerSocket  DenialReason = "host-docker-socket"
	DenialProfileImport     DenialReason = "profile-import"
	DenialPersistentDaemon  DenialReason = "persistent-daemon"
	DenialBrowserAutomation DenialReason = "browser-automation"
	DenialIntegrationEnv    DenialReason = "integration-env"
	DenialLANBind           DenialReason = "lan-bind"
	DenialMissingPortToken  DenialReason = "missing-port-token"
	DenialNativeContainer   DenialReason = "native-container-service"
	DenialUntypedCredential DenialReason = "untyped-credential"
	DenialInvalidRequest    DenialReason = "invalid-request"
)

type UnsupportedRequestError struct {
	Denials []DenialReason
}

func (e UnsupportedRequestError) Error() string {
	if len(e.Denials) == 0 {
		return "unsupported service harness request"
	}
	parts := make([]string, len(e.Denials))
	for i, denial := range e.Denials {
		parts[i] = string(denial)
	}
	sort.Strings(parts)
	return "unsupported service harness request: " + strings.Join(parts, ", ")
}

func (e UnsupportedRequestError) Has(reason DenialReason) bool {
	for _, denial := range e.Denials {
		if denial == reason {
			return true
		}
	}
	return false
}

type Request struct {
	AdapterID         string
	SessionID         string
	Backend           Backend
	RequiresContainer bool
	Features          FeatureRequests
	Attach            AttachPlan
	Credentials       CredentialPlan
}

type FeatureRequests struct {
	HostDockerSocket  bool
	ProfileImport     bool
	PersistentDaemon  bool
	BrowserAutomation bool
	IntegrationEnv    bool
}

type AttachPlan struct {
	Kind         AttachKind
	SessionToken string
}

type CredentialPlan struct {
	Mode CredentialMode
	IDs  []string
}

type Metadata struct {
	AdapterID string
	SessionID string
	Backend   Backend
	Attach    AttachPlan
}

type Residue struct {
	SessionID         string
	ServiceResidue    bool
	CredentialResidue bool
	AttachResidue     bool
}

func (r Residue) Present() bool {
	return r.ServiceResidue || r.CredentialResidue || r.AttachResidue
}

type MetadataStore interface {
	ListResidue(context.Context) ([]Residue, error)
	CleanupResidue(context.Context, Residue) error
	RecordPlanned(context.Context, Metadata) error
	RecordReady(context.Context, string, AttachPlan) error
	RecordStopped(context.Context, string) error
	RecordCleanupFailure(context.Context, string, error) error
}

type CredentialManager interface {
	Materialize(context.Context, string, CredentialPlan) error
	Cleanup(context.Context, string, CredentialPlan) error
}

type Service interface {
	Start(context.Context, StartRequest) (Instance, error)
}

type StartRequest struct {
	AdapterID   string
	SessionID   string
	Backend     Backend
	Credentials CredentialPlan
}

type Instance interface {
	Health(context.Context) error
	Attach(context.Context, AttachPlan) error
	Stop(context.Context) error
}

type Logger interface {
	Event(Event)
}

type Event struct {
	Phase  string
	Fields map[string]string
}

type Runner struct {
	Store       MetadataStore
	Credentials CredentialManager
	Logger      Logger
}

type Result struct {
	SessionID              string
	Rejected               bool
	ResidueRecovered       int
	MetadataRecorded       bool
	CredentialMaterialized bool
	CredentialCleaned      bool
	ServiceStarted         bool
	Ready                  bool
	Attached               bool
	Stopped                bool
	CleanupFailures        int
}

func (r Runner) Run(ctx context.Context, req Request, service Service) (Result, error) {
	result := Result{SessionID: req.SessionID}
	if err := ValidateRequest(req); err != nil {
		result.Rejected = true
		r.log("reject", map[string]string{
			"adapter_id": req.AdapterID,
			"reason":     err.Error(),
		})
		return result, err
	}
	if r.Store == nil {
		return result, errors.New("service harness metadata store is required")
	}
	if service == nil {
		return result, errors.New("service harness service is required")
	}
	if req.Credentials.Mode == CredentialTyped && r.Credentials == nil {
		return result, errors.New("service harness credential manager is required")
	}

	recovered, err := r.cleanupPriorResidue(ctx)
	result.ResidueRecovered = recovered
	if err != nil {
		result.CleanupFailures++
		return result, err
	}

	metadata := Metadata{
		AdapterID: req.AdapterID,
		SessionID: req.SessionID,
		Backend:   req.Backend,
		Attach:    req.Attach,
	}
	if err := r.Store.RecordPlanned(ctx, metadata); err != nil {
		return result, fmt.Errorf("record service plan: %w", err)
	}
	result.MetadataRecorded = true
	r.log("planned", map[string]string{
		"adapter_id": req.AdapterID,
		"session_id": req.SessionID,
		"backend":    string(req.Backend),
	})

	if req.Credentials.Mode == CredentialTyped {
		if err := r.Credentials.Materialize(ctx, req.SessionID, req.Credentials); err != nil {
			return result, fmt.Errorf("materialize service credentials: %w", err)
		}
		result.CredentialMaterialized = true
		r.log("credentials.materialized", map[string]string{
			"session_id":     req.SessionID,
			"credential_ids": strings.Join(req.Credentials.IDs, ","),
		})
	}

	handle, err := service.Start(ctx, StartRequest{
		AdapterID:   req.AdapterID,
		SessionID:   req.SessionID,
		Backend:     req.Backend,
		Credentials: req.Credentials,
	})
	if handle != nil {
		result.ServiceStarted = true
	}
	if err != nil {
		return result, errors.Join(err, r.cleanupCurrent(ctx, req, handle, &result))
	}
	if handle == nil {
		return result, errors.Join(errors.New("service start returned no instance"), r.cleanupCurrent(ctx, req, nil, &result))
	}
	r.log("started", map[string]string{
		"adapter_id": req.AdapterID,
		"session_id": req.SessionID,
	})

	if err := handle.Health(ctx); err != nil {
		return result, errors.Join(fmt.Errorf("service health check: %w", err), r.cleanupCurrent(ctx, req, handle, &result))
	}
	result.Ready = true
	if err := r.Store.RecordReady(ctx, req.SessionID, req.Attach); err != nil {
		return result, errors.Join(fmt.Errorf("record service ready: %w", err), r.cleanupCurrent(ctx, req, handle, &result))
	}
	r.log("ready", map[string]string{
		"adapter_id":    req.AdapterID,
		"session_id":    req.SessionID,
		"attach_kind":   string(req.Attach.Kind),
		"session_token": req.Attach.SessionToken,
	})

	if err := handle.Attach(ctx, req.Attach); err != nil {
		return result, errors.Join(fmt.Errorf("service attach: %w", err), r.cleanupCurrent(ctx, req, handle, &result))
	}
	result.Attached = true
	r.log("attached", map[string]string{
		"adapter_id":  req.AdapterID,
		"session_id":  req.SessionID,
		"attach_kind": string(req.Attach.Kind),
	})

	if err := r.cleanupCurrent(ctx, req, handle, &result); err != nil {
		return result, err
	}
	return result, nil
}

func ValidateRequest(req Request) error {
	var denials []DenialReason
	if strings.TrimSpace(req.AdapterID) == "" || strings.TrimSpace(req.SessionID) == "" {
		denials = append(denials, DenialInvalidRequest)
	}
	if !validBackend(req.Backend) || !validAttachKind(req.Attach.Kind) || !validCredentialMode(req.Credentials.Mode) {
		denials = append(denials, DenialInvalidRequest)
	}
	if req.Features.HostDockerSocket {
		denials = append(denials, DenialHostDockerSocket)
	}
	if req.Features.ProfileImport {
		denials = append(denials, DenialProfileImport)
	}
	if req.Features.PersistentDaemon {
		denials = append(denials, DenialPersistentDaemon)
	}
	if req.Features.BrowserAutomation {
		denials = append(denials, DenialBrowserAutomation)
	}
	if req.Features.IntegrationEnv {
		denials = append(denials, DenialIntegrationEnv)
	}
	if req.Attach.Kind == AttachLANPort {
		denials = append(denials, DenialLANBind)
	}
	if req.Attach.Kind == AttachLocalhostPort && strings.TrimSpace(req.Attach.SessionToken) == "" {
		denials = append(denials, DenialMissingPortToken)
	}
	if req.RequiresContainer && req.Backend == BackendNative {
		denials = append(denials, DenialNativeContainer)
	}
	if req.Credentials.Mode == CredentialUntyped {
		denials = append(denials, DenialUntypedCredential)
	}
	if len(denials) > 0 {
		return UnsupportedRequestError{Denials: denials}
	}
	return nil
}

func (r Runner) cleanupPriorResidue(ctx context.Context) (int, error) {
	residues, err := r.Store.ListResidue(ctx)
	if err != nil {
		return 0, fmt.Errorf("list service residue: %w", err)
	}
	cleaned := 0
	for _, residue := range residues {
		if !residue.Present() {
			continue
		}
		if err := r.Store.CleanupResidue(ctx, residue); err != nil {
			recordErr := r.Store.RecordCleanupFailure(ctx, residue.SessionID, err)
			return cleaned, errors.Join(fmt.Errorf("cleanup stale service residue %s: %w", residue.SessionID, err), recordErr)
		}
		cleaned++
		r.log("residue.cleaned", map[string]string{"session_id": residue.SessionID})
	}
	return cleaned, nil
}

func (r Runner) cleanupCurrent(ctx context.Context, req Request, handle Instance, result *Result) error {
	var errs []error
	if handle != nil {
		if err := handle.Stop(ctx); err != nil {
			result.CleanupFailures++
			errs = append(errs, fmt.Errorf("stop service: %w", err))
			errs = append(errs, r.Store.RecordCleanupFailure(ctx, req.SessionID, err))
		} else {
			result.Stopped = true
			if err := r.Store.RecordStopped(ctx, req.SessionID); err != nil {
				errs = append(errs, fmt.Errorf("record service stopped: %w", err))
			}
			r.log("stopped", map[string]string{"session_id": req.SessionID})
		}
	}
	if result.CredentialMaterialized {
		if err := r.Credentials.Cleanup(ctx, req.SessionID, req.Credentials); err != nil {
			result.CleanupFailures++
			errs = append(errs, fmt.Errorf("cleanup service credentials: %w", err))
			errs = append(errs, r.Store.RecordCleanupFailure(ctx, req.SessionID, err))
		} else {
			result.CredentialCleaned = true
			r.log("credentials.cleaned", map[string]string{"session_id": req.SessionID})
		}
	}
	return errors.Join(errs...)
}

func (r Runner) log(phase string, fields map[string]string) {
	if r.Logger == nil {
		return
	}
	r.Logger.Event(Event{Phase: phase, Fields: redactFields(fields)})
}

func redactFields(fields map[string]string) map[string]string {
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		if sensitiveLogField(key) && value != "" {
			out[key] = "[redacted]"
			continue
		}
		out[key] = value
	}
	return out
}

func sensitiveLogField(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "cookie")
}

func validBackend(backend Backend) bool {
	switch backend {
	case BackendNative, BackendDockerSandbox, BackendVM:
		return true
	default:
		return false
	}
}

func validAttachKind(kind AttachKind) bool {
	switch kind {
	case AttachStdio, AttachUnixSocket, AttachLocalhostPort, AttachLANPort:
		return true
	default:
		return false
	}
}

func validCredentialMode(mode CredentialMode) bool {
	switch mode {
	case CredentialNone, CredentialTyped, CredentialUntyped:
		return true
	default:
		return false
	}
}
