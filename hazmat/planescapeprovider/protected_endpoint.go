package planescapeprovider

import "context"

// ProtectedBrokerDiscoveryEndpointConfigV1 composes one protected broker
// dialer with immutable authenticated-client authority.
type ProtectedBrokerDiscoveryEndpointConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// NewProtectedBrokerDiscoveryEndpointV1 composes the authenticated discovery
// transport and provider-v1 codec into the existing product Endpoint. The
// discovery-only transport rejects lifecycle frames before dialing.
func NewProtectedBrokerDiscoveryEndpointV1(
	config ProtectedBrokerDiscoveryEndpointConfigV1,
) (Endpoint, error) {
	transport, err := NewProtectedBrokerDiscoveryTransportV1(
		ProtectedBrokerDiscoveryTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport: transport,
		Codec:     ProviderV1FrameCodec{},
	})
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return endpoint, nil
}

// ProtectedBrokerEndpointConfigV1 composes productive discovery, compiled-plan
// admission, Tool, Pause, Freeze, Closeout, and Cancellation over fresh
// authenticated connections to the same exact protected-broker address.
type ProtectedBrokerEndpointConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// ProtectedBrokerEndpointV1 exposes only lifecycle effects with a published
// authenticated RPC. Unsupported kinds remain unreachable before dialing.
type ProtectedBrokerEndpointV1 struct {
	discovery    Endpoint
	admission    *ProtectedBrokerAdmissionTransportV1
	tool         *ProtectedBrokerToolTransportV1
	quiescence   *ProtectedBrokerQuiescenceTransportV1
	freeze       *ProtectedBrokerFreezeTransportV1
	closeout     *ProtectedBrokerCloseoutTransportV1
	cancellation *ProtectedBrokerCancellationTransportV1
	backend      BackendIdentityBinding
}

var _ Endpoint = (*ProtectedBrokerEndpointV1)(nil)
var _ BoundEndpoint = (*ProtectedBrokerEndpointV1)(nil)

func NewProtectedBrokerEndpointV1(
	config ProtectedBrokerEndpointConfigV1,
) (*ProtectedBrokerEndpointV1, error) {
	discovery, err := NewProtectedBrokerDiscoveryEndpointV1(
		ProtectedBrokerDiscoveryEndpointConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	admission, err := NewProtectedBrokerAdmissionTransportV1(
		ProtectedBrokerAdmissionTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	tool, err := NewProtectedBrokerToolTransportV1(
		ProtectedBrokerToolTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	quiescence, err := NewProtectedBrokerQuiescenceTransportV1(
		ProtectedBrokerQuiescenceTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	freeze, err := NewProtectedBrokerFreezeTransportV1(
		ProtectedBrokerFreezeTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	closeout, err := NewProtectedBrokerCloseoutTransportV1(
		ProtectedBrokerCloseoutTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	cancellation, err := NewProtectedBrokerCancellationTransportV1(
		ProtectedBrokerCancellationTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	return &ProtectedBrokerEndpointV1{
		discovery:    discovery,
		admission:    admission,
		tool:         tool,
		quiescence:   quiescence,
		freeze:       freeze,
		closeout:     closeout,
		cancellation: cancellation,
		backend:      config.Client.BackendBinding(),
	}, nil
}

func (e *ProtectedBrokerEndpointV1) BackendBinding() BackendIdentityBinding {
	if e == nil {
		return BackendIdentityBinding{}
	}
	return e.backend
}

func (e *ProtectedBrokerEndpointV1) Discover(
	ctx context.Context,
) (ProviderCapabilities, error) {
	if !e.usable() {
		return ProviderCapabilities{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return e.discovery.Discover(ctx)
}

func (e *ProtectedBrokerEndpointV1) Admit(
	ctx context.Context,
	plan CompiledContainmentPlan,
) (SessionAdmission, error) {
	if !e.usable() {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return e.admission.Admit(ctx, plan)
}

func (e *ProtectedBrokerEndpointV1) Operate(
	ctx context.Context,
	operation AgentOperation,
) (OperationResponse, error) {
	if !e.usable() {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	switch operation.Kind() {
	case OperationTool:
		result, err := e.tool.Operate(ctx, operation)
		if err != nil {
			return nil, err
		}
		return result, nil
	case OperationPause:
		result, err := e.quiescence.Operate(ctx, operation)
		if err != nil {
			return nil, err
		}
		return result, nil
	case OperationCloseout:
		result, err := e.closeout.Closeout(ctx, operation)
		if err != nil {
			return nil, err
		}
		return result, nil
	default:
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
}

func (e *ProtectedBrokerEndpointV1) Freeze(
	ctx context.Context,
	request Freeze,
) (FreezeAck, error) {
	if !e.usable() {
		return FreezeAck{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return e.freeze.Freeze(ctx, request)
}

func (e *ProtectedBrokerEndpointV1) Cancel(
	ctx context.Context,
	request Cancellation,
) (CancellationAck, error) {
	if !e.usable() {
		return CancellationAck{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return e.cancellation.Cancel(ctx, request)
}

func (e *ProtectedBrokerEndpointV1) usable() bool {
	return e != nil && e.discovery != nil && e.admission != nil &&
		e.tool != nil && e.quiescence != nil && e.freeze != nil &&
		e.closeout != nil && e.cancellation != nil && e.backend.valid()
}
