package planescapeprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// FrameTransport exchanges one bounded JSON provider frame. It is deliberately
// narrower than a network client so Unix-socket, TLS, and test transports can
// share the same fail-closed record boundary.
type FrameTransport interface {
	RoundTrip(context.Context, []byte) ([]byte, error)
}

// FrameCodec owns the released provider-v1 envelope mapping. The schema corpus
// freezes record contents, but the productive Linux endpoint is not yet
// published; keeping the envelope injected avoids inventing a parallel RPC
// protocol in Hazmat.
type FrameCodec interface {
	EncodeDiscovery() ([]byte, error)
	DecodeCapabilities([]byte) (ProviderCapabilities, error)
	EncodeCompiledContainmentPlan(CompiledContainmentPlan) ([]byte, error)
	DecodeAdmission([]byte) (SessionAdmission, error)
	EncodeOperation(AgentOperation) ([]byte, error)
	DecodeOperation([]byte) (OperationResponse, error)
	EncodeFreeze(Freeze) ([]byte, error)
	DecodeFreezeAck([]byte) (FreezeAck, error)
	EncodeCancellation(Cancellation) ([]byte, error)
	DecodeCancellationAck([]byte) (CancellationAck, error)
}

type FramedEndpointConfig struct {
	Transport FrameTransport
	Codec     FrameCodec
}

// FramedEndpoint turns a released envelope codec into the semantic Endpoint
// consumed by Client. It validates the bounded-JSON outer frame before any DTO
// reaches the lifecycle client.
type FramedEndpoint struct {
	transport FrameTransport
	codec     FrameCodec
}

var _ Endpoint = (*FramedEndpoint)(nil)

func NewFramedEndpoint(config FramedEndpointConfig) (*FramedEndpoint, error) {
	if config.Transport == nil || config.Codec == nil {
		return nil, fmt.Errorf("planescapeprovider: framed transport and codec are required")
	}
	return &FramedEndpoint{transport: config.Transport, codec: config.Codec}, nil
}

func (e *FramedEndpoint) Discover(ctx context.Context) (ProviderCapabilities, error) {
	if !e.usable() {
		return ProviderCapabilities{}, framingError{class: ErrorUnavailable}
	}
	request, err := e.codec.EncodeDiscovery()
	if err != nil {
		return ProviderCapabilities{}, framingError{class: ErrorInvalid}
	}
	response, err := e.roundTrip(ctx, request)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	value, err := e.codec.DecodeCapabilities(response)
	if err != nil {
		return ProviderCapabilities{}, decodeError(err)
	}
	return value, nil
}

func (e *FramedEndpoint) Admit(
	ctx context.Context,
	request CompiledContainmentPlan,
) (SessionAdmission, error) {
	if !e.usable() {
		return SessionAdmission{}, framingError{class: ErrorUnavailable}
	}
	if !request.valid() {
		return SessionAdmission{}, framingError{class: ErrorInvalid}
	}
	frame, err := e.codec.EncodeCompiledContainmentPlan(request)
	if err != nil {
		return SessionAdmission{}, framingError{class: ErrorInvalid}
	}
	response, err := e.roundTrip(ctx, frame)
	if err != nil {
		return SessionAdmission{}, err
	}
	value, err := e.codec.DecodeAdmission(response)
	if err != nil {
		return SessionAdmission{}, decodeError(err)
	}
	return value, nil
}

func (e *FramedEndpoint) Operate(ctx context.Context, request AgentOperation) (OperationResponse, error) {
	if !e.usable() {
		return nil, framingError{class: ErrorUnavailable}
	}
	if !request.valid() {
		return nil, framingError{class: ErrorInvalid}
	}
	frame, err := e.codec.EncodeOperation(request)
	if err != nil {
		return nil, framingError{class: ErrorInvalid}
	}
	response, err := e.roundTrip(ctx, frame)
	if err != nil {
		return nil, err
	}
	value, err := e.codec.DecodeOperation(response)
	if err != nil {
		return nil, decodeError(err)
	}
	return value, nil
}

func (e *FramedEndpoint) Freeze(ctx context.Context, request Freeze) (FreezeAck, error) {
	if !e.usable() {
		return FreezeAck{}, framingError{class: ErrorUnavailable}
	}
	if !request.valid() {
		return FreezeAck{}, framingError{class: ErrorInvalid}
	}
	frame, err := e.codec.EncodeFreeze(request)
	if err != nil {
		return FreezeAck{}, framingError{class: ErrorInvalid}
	}
	response, err := e.roundTrip(ctx, frame)
	if err != nil {
		return FreezeAck{}, err
	}
	value, err := e.codec.DecodeFreezeAck(response)
	if err != nil {
		return FreezeAck{}, decodeError(err)
	}
	return value, nil
}

func (e *FramedEndpoint) Cancel(ctx context.Context, request Cancellation) (CancellationAck, error) {
	if !e.usable() {
		return CancellationAck{}, framingError{class: ErrorUnavailable}
	}
	if !request.valid() {
		return CancellationAck{}, framingError{class: ErrorInvalid}
	}
	frame, err := e.codec.EncodeCancellation(request)
	if err != nil {
		return CancellationAck{}, framingError{class: ErrorInvalid}
	}
	response, err := e.roundTrip(ctx, frame)
	if err != nil {
		return CancellationAck{}, err
	}
	value, err := e.codec.DecodeCancellationAck(response)
	if err != nil {
		return CancellationAck{}, decodeError(err)
	}
	return value, nil
}

func (e *FramedEndpoint) roundTrip(ctx context.Context, request []byte) ([]byte, error) {
	if !e.usable() {
		return nil, framingError{class: ErrorUnavailable}
	}
	if !validJSONFrame(request) {
		return nil, framingError{class: ErrorInvalid}
	}
	response, err := e.transport.RoundTrip(ctx, append([]byte(nil), request...))
	if err != nil {
		return nil, err
	}
	if !validJSONFrame(response) {
		return nil, framingError{class: ErrorConflict}
	}
	return append([]byte(nil), response...), nil
}

func (e *FramedEndpoint) usable() bool {
	return e != nil && e.transport != nil && e.codec != nil
}

func validJSONFrame(frame []byte) bool {
	return len(frame) > 0 && len(frame) <= MaxRecordBytes && json.Valid(frame)
}

type framingError struct {
	class ErrorClass
}

func (e framingError) Error() string { return "planescape provider framing rejected" }

func decodeError(err error) error {
	if err == nil {
		return framingError{class: ErrorConflict}
	}
	var failure *ProviderFailure
	if errors.As(err, &failure) {
		return failure
	}
	return framingError{class: ErrorConflict}
}
