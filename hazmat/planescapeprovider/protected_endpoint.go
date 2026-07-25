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

// ProtectedBrokerEndpointConfigV1 composes productive discovery and compiled-
// plan admission over fresh authenticated connections to the same exact
// protected-broker address.
type ProtectedBrokerEndpointConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// ProtectedBrokerEndpointV1 exposes only the protected lifecycle surface that
// has a published authenticated RPC. Later lifecycle methods remain
// unreachable before the dial boundary.
type ProtectedBrokerEndpointV1 struct {
	discovery Endpoint
	admission *ProtectedBrokerAdmissionTransportV1
}

var _ Endpoint = (*ProtectedBrokerEndpointV1)(nil)

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
	return &ProtectedBrokerEndpointV1{
		discovery: discovery,
		admission: admission,
	}, nil
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
	context.Context,
	AgentOperation,
) (OperationResponse, error) {
	if !e.usable() {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
}

func (e *ProtectedBrokerEndpointV1) Freeze(
	context.Context,
	Freeze,
) (FreezeAck, error) {
	if !e.usable() {
		return FreezeAck{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return FreezeAck{}, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
}

func (e *ProtectedBrokerEndpointV1) Cancel(
	context.Context,
	Cancellation,
) (CancellationAck, error) {
	if !e.usable() {
		return CancellationAck{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return CancellationAck{}, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
}

func (e *ProtectedBrokerEndpointV1) usable() bool {
	return e != nil && e.discovery != nil && e.admission != nil
}
