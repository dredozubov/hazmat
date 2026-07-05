package proxyruntime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"hazmat/sessionbackend"
)

type ServiceKind string

const (
	ServiceKindProxyAPI     ServiceKind = "api-proxy"
	ServiceKindProxyHTTPMCP ServiceKind = "http-mcp-proxy"
)

type ServiceCredentialMode string

const (
	ServiceCredentialNone    ServiceCredentialMode = "none"
	ServiceCredentialTyped   ServiceCredentialMode = "typed"
	ServiceCredentialUntyped ServiceCredentialMode = "untyped"
)

type ServiceDenialReason string

const (
	ServiceDenialInvalidRequest    ServiceDenialReason = "invalid-request"
	ServiceDenialProfileImport     ServiceDenialReason = "profile-import"
	ServiceDenialPersistentDaemon  ServiceDenialReason = "persistent-daemon"
	ServiceDenialBrowserAutomation ServiceDenialReason = "browser-automation"
	ServiceDenialIntegrationEnv    ServiceDenialReason = "integration-env"
	ServiceDenialNonLocalBind      ServiceDenialReason = "non-local-bind"
	ServiceDenialMissingToken      ServiceDenialReason = "missing-local-session-token"
	ServiceDenialNativeContainer   ServiceDenialReason = "native-container-service"
	ServiceDenialUntypedCredential ServiceDenialReason = "untyped-credential"
)

type UnsupportedServiceRequestError struct {
	Denials []ServiceDenialReason
}

func (e UnsupportedServiceRequestError) Error() string {
	if len(e.Denials) == 0 {
		return "unsupported proxy service request"
	}
	parts := make([]string, len(e.Denials))
	for i, denial := range e.Denials {
		parts[i] = string(denial)
	}
	sort.Strings(parts)
	return "unsupported proxy service request: " + strings.Join(parts, ", ")
}

func (e UnsupportedServiceRequestError) Has(reason ServiceDenialReason) bool {
	for _, denial := range e.Denials {
		if denial == reason {
			return true
		}
	}
	return false
}

type ServiceRequest struct {
	ServiceKind       ServiceKind
	SessionID         string
	ProxyKind         ProxyKind
	Downstream        DownstreamIdentity
	Backend           sessionbackend.Kind
	RequiresContainer bool
	Features          ServiceFeatureRequests
	Attach            ServiceAttach
	Credentials       ServiceCredentialPlan
}

type ServiceFeatureRequests struct {
	ProfileImport     bool
	PersistentDaemon  bool
	BrowserAutomation bool
	IntegrationEnv    bool
}

type ServiceAttach struct {
	Kind         AttachKind
	Address      string
	SessionToken string
}

type ServiceCredentialPlan struct {
	Mode ServiceCredentialMode
	IDs  []string
}

type ServiceMetadata struct {
	ServiceKind ServiceKind
	SessionID   string
	ProxyKind   ProxyKind
	Downstream  DownstreamIdentity
	Backend     sessionbackend.Kind
	Attach      ServiceAttach
}

type ServiceResidue struct {
	SessionID         string
	ServiceResidue    bool
	CredentialResidue bool
	AttachResidue     bool
}

func (r ServiceResidue) Present() bool {
	return r.ServiceResidue || r.CredentialResidue || r.AttachResidue
}

type ServiceMetadataStore interface {
	ListResidue(context.Context) ([]ServiceResidue, error)
	CleanupResidue(context.Context, ServiceResidue) error
	RecordPlanned(context.Context, ServiceMetadata) error
	RecordReady(context.Context, string, ServiceAttach) error
	RecordStopped(context.Context, string) error
	RecordCleanupFailure(context.Context, string, error) error
}

type ServiceCredentialManager interface {
	Materialize(context.Context, string, ServiceCredentialPlan) error
	Cleanup(context.Context, string, ServiceCredentialPlan) error
}

type Service interface {
	Start(context.Context, ServiceStartRequest) (ServiceInstance, error)
}

type ServiceStartRequest struct {
	ServiceKind ServiceKind
	SessionID   string
	ProxyKind   ProxyKind
	Downstream  DownstreamIdentity
	Backend     sessionbackend.Kind
	Credentials ServiceCredentialPlan
}

type ServiceInstance interface {
	Health(context.Context) error
	Attach(context.Context, ServiceAttach) error
	Stop(context.Context) error
}

type ServiceRunner struct {
	Store       ServiceMetadataStore
	Credentials ServiceCredentialManager
	Events      EventSink
}

type ServiceResult struct {
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
	Events                 []Event
}

func (r ServiceRunner) Run(ctx context.Context, req ServiceRequest, service Service) (ServiceResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := ServiceResult{SessionID: req.SessionID}
	emit := func(operation string, decision Decision, reason string, attrs map[string]string) {
		event := NewEvent(serviceEventInput(req, operation, decision, reason, attrs))
		result.Events = append(result.Events, event)
		if r.Events != nil {
			r.Events(event)
		}
	}

	if err := ValidateServiceRequest(req); err != nil {
		result.Rejected = true
		emit("service:reject", DecisionDeny, err.Error(), nil)
		return result, err
	}
	if r.Store == nil {
		return result, errors.New("proxyruntime: service metadata store is required")
	}
	if service == nil {
		return result, errors.New("proxyruntime: service is required")
	}
	if req.Credentials.Mode == ServiceCredentialTyped && r.Credentials == nil {
		return result, errors.New("proxyruntime: service credential manager is required")
	}

	recovered, err := r.cleanupPriorResidue(ctx, &result, emit)
	result.ResidueRecovered = recovered
	if err != nil {
		result.CleanupFailures++
		return result, err
	}

	metadata := ServiceMetadata{
		ServiceKind: req.ServiceKind,
		SessionID:   req.SessionID,
		ProxyKind:   req.ProxyKind,
		Downstream:  req.Downstream,
		Backend:     req.Backend,
		Attach:      req.Attach,
	}
	if err := r.Store.RecordPlanned(ctx, metadata); err != nil {
		return result, fmt.Errorf("record proxy service plan: %w", err)
	}
	result.MetadataRecorded = true
	emit("service:planned", DecisionObserve, "", nil)

	if req.Credentials.Mode == ServiceCredentialTyped {
		if err := r.Credentials.Materialize(ctx, req.SessionID, req.Credentials); err != nil {
			return result, fmt.Errorf("materialize proxy service credentials: %w", err)
		}
		result.CredentialMaterialized = true
		emit("service:credentials.materialized", DecisionObserve, "", map[string]string{
			"credential_ids": strings.Join(req.Credentials.IDs, ","),
		})
	}

	handle, err := service.Start(ctx, ServiceStartRequest{
		ServiceKind: req.ServiceKind,
		SessionID:   req.SessionID,
		ProxyKind:   req.ProxyKind,
		Downstream:  req.Downstream,
		Backend:     req.Backend,
		Credentials: req.Credentials,
	})
	if handle != nil {
		result.ServiceStarted = true
	}
	if err != nil {
		return result, errors.Join(err, r.cleanupCurrent(ctx, req, handle, &result, emit))
	}
	if handle == nil {
		return result, errors.Join(errors.New("proxy service start returned no instance"), r.cleanupCurrent(ctx, req, nil, &result, emit))
	}
	emit("service:started", DecisionObserve, "", nil)

	if err := handle.Health(ctx); err != nil {
		return result, errors.Join(fmt.Errorf("proxy service health check: %w", err), r.cleanupCurrent(ctx, req, handle, &result, emit))
	}
	result.Ready = true
	if err := r.Store.RecordReady(ctx, req.SessionID, req.Attach); err != nil {
		return result, errors.Join(fmt.Errorf("record proxy service ready: %w", err), r.cleanupCurrent(ctx, req, handle, &result, emit))
	}
	emit("service:ready", DecisionObserve, "", map[string]string{
		"attach_kind":    string(req.Attach.Kind),
		"attach_address": req.Attach.Address,
		"session_token":  req.Attach.SessionToken,
	})

	if err := handle.Attach(ctx, req.Attach); err != nil {
		return result, errors.Join(fmt.Errorf("proxy service attach: %w", err), r.cleanupCurrent(ctx, req, handle, &result, emit))
	}
	result.Attached = true
	emit("service:attached", DecisionObserve, "", map[string]string{
		"attach_kind": string(req.Attach.Kind),
	})

	if err := r.cleanupCurrent(ctx, req, handle, &result, emit); err != nil {
		return result, err
	}
	return result, nil
}

func ValidateServiceRequest(req ServiceRequest) error {
	var denials []ServiceDenialReason
	if strings.TrimSpace(req.SessionID) == "" || !validServiceKind(req.ServiceKind) ||
		!validProxyKind(req.ProxyKind) || !validBackend(req.Backend) || !validServiceCredentialMode(req.Credentials.Mode) {
		denials = append(denials, ServiceDenialInvalidRequest)
	}
	if req.Features.ProfileImport {
		denials = append(denials, ServiceDenialProfileImport)
	}
	if req.Features.PersistentDaemon {
		denials = append(denials, ServiceDenialPersistentDaemon)
	}
	if req.Features.BrowserAutomation {
		denials = append(denials, ServiceDenialBrowserAutomation)
	}
	if req.Features.IntegrationEnv {
		denials = append(denials, ServiceDenialIntegrationEnv)
	}
	if !localServiceAttach(req.Attach.Kind) {
		denials = append(denials, ServiceDenialNonLocalBind)
	}
	if req.Attach.Kind == AttachKindLocalHTTP && !localHTTPServiceAddress(req.Attach.Address) {
		denials = append(denials, ServiceDenialNonLocalBind)
	}
	if req.Attach.Kind == AttachKindLocalHTTP && strings.TrimSpace(req.Attach.SessionToken) == "" {
		denials = append(denials, ServiceDenialMissingToken)
	}
	if req.RequiresContainer && nativeBackend(req.Backend) {
		denials = append(denials, ServiceDenialNativeContainer)
	}
	if req.Credentials.Mode == ServiceCredentialUntyped {
		denials = append(denials, ServiceDenialUntypedCredential)
	}
	if len(denials) > 0 {
		return UnsupportedServiceRequestError{Denials: denials}
	}
	return nil
}

func (r ServiceRunner) cleanupPriorResidue(ctx context.Context, result *ServiceResult, emit func(string, Decision, string, map[string]string)) (int, error) {
	residues, err := r.Store.ListResidue(ctx)
	if err != nil {
		return 0, fmt.Errorf("list proxy service residue: %w", err)
	}
	cleaned := 0
	for _, residue := range residues {
		if !residue.Present() {
			continue
		}
		if err := r.Store.CleanupResidue(ctx, residue); err != nil {
			recordErr := r.Store.RecordCleanupFailure(ctx, residue.SessionID, err)
			return cleaned, errors.Join(fmt.Errorf("cleanup stale proxy service residue %s: %w", residue.SessionID, err), recordErr)
		}
		cleaned++
		emit("service:residue.cleaned", DecisionObserve, "", map[string]string{"residue_session_id": residue.SessionID})
	}
	return cleaned, nil
}

func (r ServiceRunner) cleanupCurrent(ctx context.Context, req ServiceRequest, handle ServiceInstance, result *ServiceResult, emit func(string, Decision, string, map[string]string)) error {
	var errs []error
	if handle != nil {
		if err := handle.Stop(ctx); err != nil {
			result.CleanupFailures++
			errs = append(errs, fmt.Errorf("stop proxy service: %w", err))
			errs = append(errs, r.Store.RecordCleanupFailure(ctx, req.SessionID, err))
		} else {
			result.Stopped = true
			if err := r.Store.RecordStopped(ctx, req.SessionID); err != nil {
				errs = append(errs, fmt.Errorf("record proxy service stopped: %w", err))
			}
			emit("service:stopped", DecisionObserve, "", nil)
		}
	}
	if result.CredentialMaterialized {
		if err := r.Credentials.Cleanup(ctx, req.SessionID, req.Credentials); err != nil {
			result.CleanupFailures++
			errs = append(errs, fmt.Errorf("cleanup proxy service credentials: %w", err))
			errs = append(errs, r.Store.RecordCleanupFailure(ctx, req.SessionID, err))
		} else {
			result.CredentialCleaned = true
			emit("service:credentials.cleaned", DecisionObserve, "", nil)
		}
	}
	return errors.Join(errs...)
}

func serviceEventInput(req ServiceRequest, operation string, decision Decision, reason string, attrs map[string]string) EventInput {
	return EventInput{
		SessionID:  req.SessionID,
		ProxyKind:  req.ProxyKind,
		Downstream: req.Downstream,
		Backend:    req.Backend,
		AttachKind: req.Attach.Kind,
		Direction:  DirectionLifecycle,
		Operation:  operation,
		Decision:   decision,
		Reason:     reason,
		Attributes: attrs,
	}
}

func localServiceAttach(kind AttachKind) bool {
	switch kind {
	case AttachKindUnixSocket, AttachKindLocalHTTP:
		return true
	case AttachKindStdio, AttachKindRemoteHTTP:
		return false
	default:
		return false
	}
}

func localHTTPServiceAddress(address string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validServiceKind(kind ServiceKind) bool {
	switch kind {
	case ServiceKindProxyAPI, ServiceKindProxyHTTPMCP:
		return true
	default:
		return false
	}
}

func validProxyKind(kind ProxyKind) bool {
	switch kind {
	case ProxyKindMCPHTTP, ProxyKindLLMHTTP:
		return true
	case ProxyKindMCPStdio:
		return false
	default:
		return false
	}
}

func validBackend(backend sessionbackend.Kind) bool {
	switch backend {
	case sessionbackend.KindDarwinNative, sessionbackend.KindLinuxNative, sessionbackend.KindDockerSandbox, sessionbackend.KindAppleContainer:
		return true
	case sessionbackend.KindUnsupportedNative, sessionbackend.KindRemoteEnvelope:
		return false
	default:
		return false
	}
}

func nativeBackend(backend sessionbackend.Kind) bool {
	return backend == sessionbackend.KindDarwinNative || backend == sessionbackend.KindLinuxNative
}

func validServiceCredentialMode(mode ServiceCredentialMode) bool {
	switch mode {
	case ServiceCredentialNone, ServiceCredentialTyped, ServiceCredentialUntyped:
		return true
	default:
		return false
	}
}
